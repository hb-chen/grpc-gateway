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

	s.handlers[meth] = append([]handler{{pat: pat, h: h}}, s.handlers[meth]...)
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
		_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
		s.routingErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, http.StatusBadRequest)
		return
	}

	components := strings.Split(path[1:], "/")

	if override := r.Header.Get("X-HTTP-Method-Override"); override != "" && s.isPathLengthFallback(r) {
		r.Method = strings.ToUpper(override)
		if err := r.ParseForm(); err != nil {
			_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
			sterr := status.Error(codes.InvalidArgument, err.Error())
			s.errorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, sterr)
			return
		}
	}

	// Verb out here is to memoize for the fallback case below
	var verb string

	s.mu.RLock()
	for _, h := range s.handlers[r.Method] {
		// If the pattern has a verb, explicitly look for a suffix in the last
		// component that matches a colon plus the verb. This allows us to
		// handle some cases that otherwise can't be correctly handled by the
		// former LastIndex case, such as when the verb literal itself contains
		// a colon. This should work for all cases that have run through the
		// parser because we know what verb we're looking for, however, there
		// are still some cases that the parser itself cannot disambiguate. See
		// the comment there if interested.
		patVerb := h.pat.Verb()
		l := len(components)
		lastComponent := components[l-1]
		var idx int = -1
		if patVerb != "" && strings.HasSuffix(lastComponent, ":"+patVerb) {
			idx = len(lastComponent) - len(patVerb) - 1
		}
		if idx == 0 {
			s.mu.RUnlock()
			_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
			s.routingErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, http.StatusNotFound)
			return
		}
		if idx > 0 {
			components[l-1], verb = lastComponent[:idx], lastComponent[idx+1:]
		}

		pathParams, err := h.pat.Match(components, verb)
		if err != nil {
			continue
		}
		s.mu.RUnlock()
		h.h(w, r, pathParams)
		return
	}

	// lookup other methods to handle fallback from GET to POST and
	// to determine if it is NotImplemented or NotFound.
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
					_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
					sterr := status.Error(codes.InvalidArgument, err.Error())
					s.errorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, sterr)
					return
				}
				h.h(w, r, pathParams)
				return
			}
			_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
			s.routingErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, http.StatusMethodNotAllowed)
			return
		}
	}
	s.mu.RUnlock()

	_, outboundMarshaler := MarshalerForRequest(s.ServeMux, r)
	s.routingErrorHandler(ctx, s.ServeMux, outboundMarshaler, w, r, http.StatusNotFound)
}

func NewServeMuxDynamic(opts ...ServeMuxOption) *ServeMuxDynamic {
	return &ServeMuxDynamic{
		ServeMux: NewServeMux(opts...),
	}
}
