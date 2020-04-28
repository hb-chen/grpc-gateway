package runtime

import (
	"net/http"
	"strings"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ServeMuxDynamic struct {
	*ServeMux

	mu sync.RWMutex
}

// Handle associates "h" to the pair of HTTP method and path pattern.
func (s *ServeMuxDynamic) Handle(meth string, pat Pattern, h HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	handlers := s.handlers[meth]

	offset := 0
	newHandlers := make([]handler, 0, len(handlers))
	for idx, h := range handlers {
		if h.pat.String() == pat.String() {
			newHandlers = append(newHandlers, handlers[offset:idx]...)
			offset = idx + 1
		}
	}

	if offset == 0 {
		s.handlers[meth] = append(handlers, handler{pat: pat, h: h})
	} else {
		newHandlers = append(newHandlers, handlers[offset:]...)
		newHandlers = append(newHandlers, handler{pat: pat, h: h})

		s.handlers[meth] = newHandlers
	}
}

// Handler deregister with method and path pattern.
func (s *ServeMuxDynamic) HandlerDeregister(meth string, pat Pattern) {
	s.mu.Lock()
	defer s.mu.Unlock()

	handlers := s.handlers[meth]
	if len(handlers) == 0 {
		return
	}

	offset := 0
	newHandlers := make([]handler, 0, len(handlers))
	for idx, h := range handlers {
		if h.pat.String() == pat.String() {
			newHandlers = append(newHandlers, handlers[offset:idx]...)
			offset = idx + 1
		}
	}
	newHandlers = append(newHandlers, handlers[offset:]...)

	s.handlers[meth] = newHandlers
}

// ServeHTTP dispatches the request to the first handler whose pattern matches to r.Method and r.Path.
func (s *ServeMuxDynamic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := r.URL.Path
	if !strings.HasPrefix(path, "/") {
		if s.protoErrorHandler != nil {
			_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
			sterr := status.Error(codes.InvalidArgument, http.StatusText(http.StatusBadRequest))
			s.protoErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, sterr)
		} else {
			OtherErrorHandler(w, r, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}
		return
	}

	components := strings.Split(path[1:], "/")
	l := len(components)
	var verb string
	if idx := strings.LastIndex(components[l-1], ":"); idx == 0 {
		if s.protoErrorHandler != nil {
			_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
			s.protoErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, ErrUnknownURI)
		} else {
			OtherErrorHandler(w, r, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		}
		return
	} else if idx > 0 {
		c := components[l-1]
		components[l-1], verb = c[:idx], c[idx+1:]
	}

	if override := r.Header.Get("X-HTTP-Method-Override"); override != "" && s.isPathLengthFallback(r) {
		r.Method = strings.ToUpper(override)
		if err := r.ParseForm(); err != nil {
			if s.protoErrorHandler != nil {
				_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
				sterr := status.Error(codes.InvalidArgument, err.Error())
				s.protoErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, sterr)
			} else {
				OtherErrorHandler(w, r, err.Error(), http.StatusBadRequest)
			}
			return
		}
	}

	s.mu.RLock()
	for _, h := range s.handlers[r.Method] {
		pathParams, err := h.pat.Match(components, verb)
		if err != nil {
			continue
		}
		s.mu.RUnlock()

		h.h(w, r, pathParams)
		return
	}

	// lookup other methods to handle fallback from GET to POST and
	// to determine if it is MethodNotAllowed or NotFound.
	for m, handlers := range s.handlers {
		if m == r.Method {
			continue
		}
		for _, h := range handlers {
			pathParams, err := h.pat.Match(components, verb)
			if err != nil {
				continue
			}
			s.mu.RUnlock()

			// X-HTTP-Method-Override is optional. Always allow fallback to POST.
			if s.isPathLengthFallback(r) {
				if err := r.ParseForm(); err != nil {
					if s.protoErrorHandler != nil {
						_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
						sterr := status.Error(codes.InvalidArgument, err.Error())
						s.protoErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, sterr)
					} else {
						OtherErrorHandler(w, r, err.Error(), http.StatusBadRequest)
					}
					return
				}
				h.h(w, r, pathParams)
				return
			}
			if s.protoErrorHandler != nil {
				_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
				s.protoErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, ErrUnknownURI)
			} else {
				OtherErrorHandler(w, r, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			}
			return
		}
	}
	s.mu.RUnlock()

	if s.protoErrorHandler != nil {
		_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
		s.protoErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, ErrUnknownURI)
	} else {
		OtherErrorHandler(w, r, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

func NewServeMuxDynamic(opts ...ServeMuxOption) *ServeMuxDynamic {
	return &ServeMuxDynamic{
		ServeMux: NewServeMux(opts...),
	}
}
