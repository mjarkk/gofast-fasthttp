package gofast

import (
	"bytes"
	"log"
	"net/http"

	"github.com/valyala/fasthttp"
)

// Handler is implements http.Handler and provide logger changing method.
type Handler interface {
	http.Handler
	SetLogger(logger *log.Logger)
}

// NewHandler returns the default Handler implementation. This default Handler
// act as the "web server" component in fastcgi specification, which connects
// fastcgi "application" through the network/address and passthrough I/O as
// specified.
func NewHandler(sessionHandler SessionHandler, clientFactory ClientFactory) func(ctx *fasthttp.RequestCtx) {
	handler := &defaultHandler{
		sessionHandler: sessionHandler,
		newClient:      clientFactory,
	}

	return func(ctx *fasthttp.RequestCtx) {
		// TODO: separate dial logic to pool client / connection
		c, err := handler.newClient()
		if err != nil {
			ctx.Error("failed to connect to FastCGI application", http.StatusBadGateway)
			log.Printf("gofast: unable to connect to FastCGI application. %s", err.Error())
			return
		}

		// defer closing with error reporting
		defer func() {
			if c == nil {
				return
			}

			// signal to close the client
			// or the pool to return the client
			if err = c.Close(); err != nil {
				log.Printf("gofast: error closing client: %s", err.Error())
			}
		}()

		// handle the session
		resp, err := handler.sessionHandler(c, NewRequest(ctx))
		if err != nil {
			ctx.Error("failed to process request", http.StatusInternalServerError)
			log.Printf("gofast: unable to process request %s", err.Error())
			return
		}

		errBuffer := new(bytes.Buffer)
		if err = resp.WriteTo(ctx, errBuffer); err != nil {
			log.Printf("gofast: problem writing error buffer to response - %s", err)
		}

		if errBuffer.Len() > 0 {
			log.Printf("gofast: error stream from application process %s", errBuffer.String())
		}
	}
}

// defaultHandler implements Handler
type defaultHandler struct {
	sessionHandler SessionHandler
	newClient      ClientFactory
	logger         *log.Logger
}

// SetLogger implements Handler
func (h *defaultHandler) SetLogger(logger *log.Logger) {
	h.logger = logger
}
