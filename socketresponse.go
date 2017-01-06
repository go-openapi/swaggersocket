package restwebsocket

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const TimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"
const bufferBeforeChunkingSize = 2048
const maxPostHandlerReadBytes = 256 << 10

var (
	bufioReaderPool   sync.Pool
	bufioWriter2kPool sync.Pool
	bufioWriter4kPool sync.Pool
)

type response struct {
	conn          *SocketConnection
	connClosed    bool // when the connection fails, the response writer should not write anything to the connection
	cancelCtx     context.CancelFunc
	req           *http.Request // request for this response
	reqBody       io.ReadCloser
	wroteHeader   bool // reply header has been (logically) written
	wantsClose    bool // HTTP request has Connection "close"
	w             *bufio.Writer
	cw            chunkWriter
	handlerHeader http.Header
	calledHeader  bool // handler accessed handlerHeader via Header

	written             int64 // number of bytes written in body
	contentLength       int64 // explicitly-declared Content-Length; or -1
	status              int   // status code passed to WriteHeader
	requestBodyLimitHit bool
	trailers            []string

	handlerDone   atomicBool // set true when the handler exits
	closeNotifyCh <-chan bool
	dateBuf       [len(TimeFormat)]byte
	clenBuf       [10]byte
}

type atomicBool int32

func (b *atomicBool) isSet() bool { return atomic.LoadInt32((*int32)(b)) != 0 }
func (b *atomicBool) setTrue()    { atomic.StoreInt32((*int32)(b), 1) }

func (w *response) WriteHeader(code int) {
	if w.wroteHeader || w.connClosed {
		return
	}
	w.wroteHeader = true
	w.status = code

	if w.calledHeader && w.cw.header == nil {
		w.cw.header = cloneHeader(w.handlerHeader)
	}

	if cl := w.handlerHeader.Get("Content-Length"); cl != "" {
		v, err := strconv.ParseInt(cl, 10, 64)
		if err == nil && v >= 0 {
			w.contentLength = v
		} else {
			log.Printf("http: invalid Content-Length of %q", cl)
			w.handlerHeader.Del("Content-Length")
		}
	}
}

func cloneHeader(h http.Header) http.Header {
	h2 := make(http.Header, len(h))
	for k, vv := range h {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		h2[k] = vv2
	}
	return h2
}

func (w *response) Header() http.Header {
	if w.cw.header == nil && w.wroteHeader && !w.cw.wroteHeader {
		w.cw.header = cloneHeader(w.handlerHeader)
	}
	w.calledHeader = true
	return w.handlerHeader
}

func (w *response) Write(data []byte) (int, error) {
	if w.connClosed {
		return -1, errors.New("connection closed")
	}
	return w.write(len(data), data, "")
}

func (w *response) WriteString(data string) (n int, err error) {
	return w.write(len(data), nil, data)
}

func (w *response) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	// flush the bufio writer
	w.w.Flush()
	// flush the websocket connection write buffer
	w.cw.flush()
}

func (w *response) finishRequest() {
	w.handlerDone.setTrue()

	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	// flush all buffers
	w.w.Flush()
	putBufioWriter(w.w)
	w.cw.close()
	w.cw.finalflush()

	// Close the body (regardless of w.closeAfterReply) so we can
	// re-use its bufio.Reader later safely.
	w.reqBody.Close()

	if w.req.MultipartForm != nil {
		w.req.MultipartForm.RemoveAll()
	}
}

// either dataB or dataS is non-zero.
func (w *response) write(lenData int, dataB []byte, dataS string) (n int, err error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if lenData == 0 {
		return 0, nil
	}

	w.written += int64(lenData) // ignoring errors, for errorKludge
	if w.contentLength != -1 && w.written > w.contentLength {
		// if contentLength is passed by the handler
		return 0, http.ErrContentLength
	}
	if dataB != nil {
		return w.w.Write(dataB)
	}
	return w.w.WriteString(dataS)
}

func (w *response) declareTrailer(k string) {
	k = http.CanonicalHeaderKey(k)
	switch k {
	case "Transfer-Encoding", "Content-Length", "Trailer":
		// Forbidden by RFC 2616 14.40.
		return
	}
	w.trailers = append(w.trailers, k)
}

type extraHeader struct {
	contentType      string
	connection       string
	transferEncoding string
	date             []byte // written if not nil
	contentLength    []byte // written if not nil
}

// Sorted the same as extraHeader.Write's loop.
var extraHeaderKeys = [][]byte{
	[]byte("Content-Type"),
	[]byte("Connection"),
	[]byte("Transfer-Encoding"),
}

var (
	headerContentLength = []byte("Content-Length: ")
	headerDate          = []byte("Date: ")
)

func (h extraHeader) Write(w io.Writer) {
	if h.date != nil {
		w.Write(headerDate)
		w.Write(h.date)
		w.Write(crlf)
	}
	if h.contentLength != nil {
		w.Write(headerContentLength)
		w.Write(h.contentLength)
		w.Write(crlf)
	}
	for i, v := range []string{h.contentType, h.connection, h.transferEncoding} {
		if v != "" {
			w.Write(extraHeaderKeys[i])
			w.Write(colonSpace)
			w.Write([]byte(v))
			w.Write(crlf)
		}
	}
}

type chunkWriter struct {
	res         *response
	header      http.Header
	wroteHeader bool
	chunking    bool
	writer      io.WriteCloser
}

var (
	crlf       = []byte("\r\n")
	colonSpace = []byte(": ")
)

// this is called whenever response.w is written to
func (cw *chunkWriter) Write(p []byte) (n int, err error) {
	log.Printf("Writing %s", string(p))
	if !cw.wroteHeader {
		// based on p the headers can be deduced
		cw.writeHeader(p)
	}
	if cw.res.req.Method == "HEAD" {
		return len(p), nil
	}
	if cw.chunking {
		_, err := fmt.Fprintf(cw.writer, "%x\r\n", len(p))
		if err != nil {
			cw.res.conn.handleFailure()
		}
	}
	n, err = cw.writer.Write(p)
	if cw.chunking && err == nil {
		_, err = cw.writer.Write(crlf)
	}
	if err != nil {
		// handle failure of writing
		//cw.res.conn.rwc.Close()
	}
	return
}

// NextWriter returns a writer for the next message to send. The writer's Close method flushes the complete message to the network.
// There can be at most one open writer on a connection. NextWriter closes the previous writer if the application has not already done so.
func (cw *chunkWriter) flush() {
	if !cw.wroteHeader {
		cw.writeHeader(nil)
	}
	cw.writer.Close()
	w, err := cw.res.conn.conn.NextWriter(websocket.TextMessage)
	if err != nil {
		cw.res.conn.handleFailure()
	}
	cw.writer = w
}

func (cw *chunkWriter) finalflush() {
	if !cw.wroteHeader {
		cw.writeHeader(nil)
	}
	cw.writer.Close()
	cw.writer = nil
}

// writes the last chunck if chunkEncoding
func (cw *chunkWriter) close() {
	if !cw.wroteHeader {
		cw.writeHeader(nil)
	}
	if cw.chunking {
		bw := cw.writer // conn's bufio writer
		// zero chunk to mark EOF
		bw.Write([]byte("0\r\n"))
		if len(cw.res.trailers) > 0 {
			trailers := make(http.Header)
			for _, h := range cw.res.trailers {
				if vv := cw.res.handlerHeader[h]; len(vv) > 0 {
					trailers[h] = vv
				}
			}
			trailers.Write(bw) // the writer handles noting errors
		}
		// final blank line after the trailers (whether
		// present or not)
		bw.Write([]byte("\r\n"))
	}
}

func (cw *chunkWriter) writeHeader(p []byte) {
	if cw.wroteHeader {
		return
	}
	cw.wroteHeader = true

	w := cw.res
	isHEAD := w.req.Method == "HEAD"

	header := cw.header
	owned := header != nil
	if !owned {
		header = w.handlerHeader
	}
	var excludeHeader map[string]bool
	delHeader := func(key string) {
		if owned {
			header.Del(key)
			return
		}
		if _, ok := header[key]; !ok {
			return
		}
		if excludeHeader == nil {
			excludeHeader = make(map[string]bool)
		}
		excludeHeader[key] = true
	}
	var setHeader extraHeader

	trailers := false
	for _, v := range cw.header["Trailer"] {
		trailers = true
		foreachHeaderElement(v, cw.res.declareTrailer)
	}

	te := header.Get("Transfer-Encoding")
	hasTE := te != ""

	if w.handlerDone.isSet() && !trailers && !hasTE && bodyAllowedForStatus(w.status) && header.Get("Content-Length") == "" && (!isHEAD || len(p) > 0) {
		w.contentLength = int64(len(p))
		setHeader.contentLength = strconv.AppendInt(cw.res.clenBuf[:0], int64(len(p)), 10)
	}
	hasCL := w.contentLength != -1

	code := w.status
	if bodyAllowedForStatus(code) {
		// If no content type, apply sniffing algorithm to body.
		_, haveType := header["Content-Type"]
		if !haveType && !hasTE {
			setHeader.contentType = http.DetectContentType(p)
		}
	} else {
		for _, k := range suppressedHeaders(code) {
			delHeader(k)
		}
	}

	if _, ok := header["Date"]; !ok {
		setHeader.date = appendTime(cw.res.dateBuf[:0], time.Now())
	}

	if hasCL && hasTE && te != "identity" {
		log.Printf("http: WriteHeader called with both Transfer-Encoding of %q and a Content-Length of %d",
			te, w.contentLength)
		delHeader("Content-Length")
		hasCL = false
	}

	if w.req.Method == "HEAD" || !bodyAllowedForStatus(code) {
		// do nothing
	} else if code == http.StatusNoContent {
		delHeader("Transfer-Encoding")
	} else if hasCL {
		delHeader("Transfer-Encoding")
	} else if w.req.ProtoAtLeast(1, 1) {
		if hasTE && te == "identity" {
			cw.chunking = false
		} else {
			cw.chunking = true
			setHeader.transferEncoding = "chunked"
			if hasTE && te == "chunked" {
				log.Println("deleting transfer encoding")
				// We will send the chunked Transfer-Encoding header later.
				delHeader("Transfer-Encoding")
			}
		}
	} else {
		delHeader("Transfer-Encoding") // in case already set
	}

	// Cannot use Content-Length with non-identity Transfer-Encoding.
	if cw.chunking {
		delHeader("Content-Length")
	}
	if !w.req.ProtoAtLeast(1, 0) {
		return
	}

	cw.writer.Write([]byte(statusLine(w.req, code)))
	cw.header.WriteSubset(cw.writer, excludeHeader)
	setHeader.Write(cw.writer)
	cw.writer.Write(crlf)
}

func newBufioWriterSize(w io.Writer, size int) *bufio.Writer {
	pool := bufioWriterPool(size)
	if pool != nil {
		if v := pool.Get(); v != nil {
			bw := v.(*bufio.Writer)
			bw.Reset(w)
			return bw
		}
	}
	return bufio.NewWriterSize(w, size)
}

func bufioWriterPool(size int) *sync.Pool {
	switch size {
	case 2 << 10:
		return &bufioWriter2kPool
	case 4 << 10:
		return &bufioWriter4kPool
	}
	return nil
}

func bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == 204:
		return false
	case status == 304:
		return false
	}
	return true
}

func foreachHeaderElement(v string, fn func(string)) {
	v = textproto.TrimString(v)
	if v == "" {
		return
	}
	if !strings.Contains(v, ",") {
		fn(v)
		return
	}
	for _, f := range strings.Split(v, ",") {
		if f = textproto.TrimString(f); f != "" {
			fn(f)
		}
	}
}

var (
	suppressedHeaders304    = []string{"Content-Type", "Content-Length", "Transfer-Encoding"}
	suppressedHeadersNoBody = []string{"Content-Length", "Transfer-Encoding"}
)

func suppressedHeaders(status int) []string {
	switch {
	case status == 304:
		// RFC 2616 section 10.3.5: "the response MUST NOT include other entity-headers"
		return suppressedHeaders304
	case !bodyAllowedForStatus(status):
		return suppressedHeadersNoBody
	}
	return nil
}

var (
	statusMu    sync.RWMutex
	statusLines = make(map[int]string)
)

// statusLine returns a response Status-Line (RFC 2616 Section 6.1)
// for the given request and response status code.
func statusLine(req *http.Request, code int) string {
	// Fast path:
	key := code
	proto11 := req.ProtoAtLeast(1, 1)
	if !proto11 {
		key = -key
	}
	statusMu.RLock()
	line, ok := statusLines[key]
	statusMu.RUnlock()
	if ok {
		return line
	}

	// Slow path:
	proto := "HTTP/1.0"
	if proto11 {
		proto = "HTTP/1.1"
	}
	codestring := fmt.Sprintf("%03d", code)
	text := http.StatusText(code)
	if text == "" {
		text = "status code " + codestring
	}
	line = proto + " " + codestring + " " + text + "\r\n"
	if ok {
		statusMu.Lock()
		defer statusMu.Unlock()
		statusLines[key] = line
	}
	return line
}

func appendTime(b []byte, t time.Time) []byte {
	const days = "SunMonTueWedThuFriSat"
	const months = "JanFebMarAprMayJunJulAugSepOctNovDec"

	t = t.UTC()
	yy, mm, dd := t.Date()
	hh, mn, ss := t.Clock()
	day := days[3*t.Weekday():]
	mon := months[3*(mm-1):]

	return append(b,
		day[0], day[1], day[2], ',', ' ',
		byte('0'+dd/10), byte('0'+dd%10), ' ',
		mon[0], mon[1], mon[2], ' ',
		byte('0'+yy/1000), byte('0'+(yy/100)%10), byte('0'+(yy/10)%10), byte('0'+yy%10), ' ',
		byte('0'+hh/10), byte('0'+hh%10), ':',
		byte('0'+mn/10), byte('0'+mn%10), ':',
		byte('0'+ss/10), byte('0'+ss%10), ' ',
		'G', 'M', 'T')
}

func putBufioWriter(bw *bufio.Writer) {
	bw.Reset(nil)
	if pool := bufioWriterPool(bw.Available()); pool != nil {
		pool.Put(bw)
	}
}
