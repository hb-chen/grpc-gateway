package runtime

import (
	"context"
	"net/http"
	"testing"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/metadata"
)

func TestServeMuxMutex_Deregister(t *testing.T) {
	type fields struct {
		handlers                  map[string][]handler
		forwardResponseOptions    []func(context.Context, http.ResponseWriter, proto.Message) error
		marshalers                marshalerRegistry
		incomingHeaderMatcher     HeaderMatcherFunc
		outgoingHeaderMatcher     HeaderMatcherFunc
		metadataAnnotators        []func(context.Context, *http.Request) metadata.MD
		streamErrorHandler        StreamErrorHandlerFunc
		protoErrorHandler         ProtoErrorHandlerFunc
		disablePathLengthFallback bool
		lastMatchWins             bool
	}
	type args struct {
		meth string
		pat  Pattern
	}

	h1 := handler{pat: MustPattern(NewPattern(1, []int{2, 0}, []string{"a"}, ""))}
	h2 := handler{pat: MustPattern(NewPattern(1, []int{2, 0, 2, 1}, []string{"a", "b"}, ""))}
	h3 := handler{pat: MustPattern(NewPattern(1, []int{2, 0, 2, 1, 2, 2}, []string{"a", "b", "c"}, ""))}
	h4 := handler{pat: MustPattern(NewPattern(1, []int{2, 0, 2, 1, 2, 2}, []string{"a", "b", "c"}, ""))}
	h5 := handler{pat: MustPattern(NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3}, []string{"a", "b", "c", "d"}, ""))}
	h6 := handler{pat: MustPattern(NewPattern(1, []int{2, 0, 2, 1, 2, 2, 2, 3, 2, 4}, []string{"a", "b", "c", "d", "e"}, ""))}

	handlers := []handler{h1, h2, h3, h4, h5}

	tests := []struct {
		name   string
		fields fields
		args   args
		count  int
	}{
		{
			name: "first",
			fields: fields{
				handlers: map[string][]handler{
					"GET": handlers[:],
				},
			},
			args: args{
				meth: "GET",
				pat:  h1.pat,
			},
			count: 1,
		},
		{
			name: "last",
			fields: fields{
				handlers: map[string][]handler{
					"GET": handlers[:],
				},
			},
			args: args{
				meth: "GET",
				pat:  h5.pat,
			},
			count: 1,
		},
		{
			name: "multi",
			fields: fields{
				handlers: map[string][]handler{
					"GET": handlers[:],
				},
			},
			args: args{
				meth: "GET",
				pat:  h3.pat,
			},
			count: 2,
		},
		{
			name: "not exist",
			fields: fields{
				handlers: map[string][]handler{
					"GET": handlers[:],
				},
			},
			args: args{
				meth: "GET",
				pat:  h6.pat,
			},
			count: 0,
		},
	}

	for _, tt := range tests {
		_tt := tt
		t.Run(_tt.name, func(t *testing.T) {
			s := &ServeMuxDynamic{
				ServeMux: &ServeMux{
					handlers: _tt.fields.handlers,
				},
			}

			s.HandlerDeregister(_tt.args.meth, _tt.args.pat)

			for _, h := range s.handlers[_tt.args.meth] {
				if h.pat.String() == _tt.args.pat.String() {
					t.Errorf("deregister error")
				}
			}

			if len(s.handlers[_tt.args.meth]) != len(handlers)-_tt.count {
				t.Errorf("deregister error count: %d", len(s.handlers[_tt.args.meth]))
			}

		})
	}
}
