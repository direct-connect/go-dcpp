package hub

import (
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"

	"github.com/rakyll/statik/fs"
	"golang.org/x/net/http2"
)

//go:generate statik -src static -p hub -f -c ""
//go:generate mv hub/statik.go static.go
//go:generate rm -r hub

const HTTPInfoPathV0 = "/api/v0/hubinfo.json"

func (h *Hub) initHTTP() error {
	if h.tls == nil {
		return nil
	}
	htl2 := h.tls.Clone()
	htl2.NextProtos = append(h.tls.NextProtos, "h2")

	h.tls.NextProtos = append(h.tls.NextProtos, "h2", "http/1.1")

	statikFS, err := fs.New()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc(HTTPInfoPathV0, h.serveV0Stats)
	mux.Handle("/", http.FileServer(statikFS))
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s: http: %s %s (%s)\n",
			r.RemoteAddr, r.Method, r.URL, r.Header.Get("User-Agent"),
		)
		st := h.Stats()
		hd := w.Header()
		hd.Set("Server", st.Soft.Name+"/"+st.Soft.Vers)
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			h.serveIndex(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	h.h1 = &http.Server{
		Handler: root,
	}
	h.h2 = &http2.Server{}
	h.h2conf = &http2.ServeConnOpts{
		BaseConfig: &http.Server{
			TLSConfig: htl2,
			Handler:   root,
		},
	}
	return nil
}

func (h *Hub) ServeHTTP1(conn net.Conn) error {
	log.Printf("%s: using HTTP1", conn.RemoteAddr())
	// make a fake listener with only one connection
	closed := make(chan struct{})
	l := &singleListen{
		closed: closed,
		addr:   conn.LocalAddr(),
		c:      &httpConn{Conn: conn, closed: closed},
	}
	err := h.h1.Serve(l)
	if err == errHTTPStop {
		err = nil
	}
	return err
}

func (h *Hub) ServeHTTP2(conn net.Conn) error {
	log.Printf("%s: using HTTP2", conn.RemoteAddr())
	h.h2.ServeConn(conn, h.h2conf)
	return nil
}

func (h *Hub) serveIndex(w http.ResponseWriter, r *http.Request) {
	st := h.Stats()
	if st.Website != "" {
		http.Redirect(w, r, st.Website, http.StatusFound)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "text/html")
	_ = indexTmpl.Execute(w, st)
}

type userStats struct {
	Name  string `json:"name"`
	Share uint64 `json:"share,omitempty"`
}

func (h *Hub) serveV0Stats(w http.ResponseWriter, r *http.Request) {
	st := h.Stats()
	hd := w.Header()
	hd.Set("Content-Type", "application/json")

	qu := r.URL.Query()
	if qu.Get("users") != "1" {
		_ = json.NewEncoder(w).Encode(st)
		return
	}

	resp := struct {
		Stats
		UserList []userStats `json:"user_list,omitempty"`
	}{Stats: st}
	for _, p := range h.Peers() {
		u := p.User()
		resp.UserList = append(resp.UserList, userStats{
			Name: u.Name, Share: u.Share,
		})
	}
	_ = json.NewEncoder(w).Encode(resp)
}

var errHTTPStop = errors.New("http: stopping after a single connection")

var _ net.Listener = (*singleListen)(nil)

type singleListen struct {
	addr   net.Addr
	closed chan struct{}
	c      net.Conn
}

func (l *singleListen) Accept() (net.Conn, error) {
	if l.c == nil {
		<-l.closed
		return nil, errHTTPStop
	}
	c := l.c
	l.c = nil
	return c, nil
}

func (l *singleListen) Close() error {
	if l.c == nil {
		return nil
	}
	err := l.c.Close()
	l.c = nil
	return err
}

func (l *singleListen) Addr() net.Addr {
	return l.addr
}

type httpConn struct {
	net.Conn
	closed chan struct{}
}

func (c *httpConn) Close() error {
	if c.closed != nil {
		ch := c.closed
		c.closed = nil
		defer close(ch)
	}
	return c.Conn.Close()
}

var indexTmpl = template.Must(template.New("").Parse(indexTemplate))

const indexTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>{{ .Name }}</title>
	<style>
		h2 {
			margin-block-start: 0.5em;
			margin-block-end: 0;
		}
		div {
			display: inline-block;
			vertical-align: top;
			margin-left: 10px;
		}
		p {
			margin-block-start: 0.5em;
			margin-block-end: 0.5em;
		}
	</style>
</head>
<body>
	<a href="https://github.com/direct-connect/go-dcpp">
		<img src="icon.png">
	</a>
	<div>
	<h2>{{ .Name }}</h2>
	<small>
		<a href="adcs://{{index .Addr 0}}?kp={{.Keyprint}}">ADCS</a>,
		<a href="adc://{{index .Addr 0}}">ADC</a>,
		<a href="dchub://{{index .Addr 0}}">NMDC</a>,
		<a href="ircs://{{index .Addr 0}}/hub">IRCS</a>,
		<a href="irc://{{index .Addr 0}}/hub">IRC</a>
	</small><br>
	<p>Users: {{ .Users }}</p>
	<i>{{ .Desc }}</i>
	</div>
</body>
</html>`
