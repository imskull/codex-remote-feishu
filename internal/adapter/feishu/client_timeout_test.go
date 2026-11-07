package feishu

import (
	"context"
	"fmt"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type clientLogRecorder struct{ messages []string }

func (r *clientLogRecorder) Debug(context.Context, ...interface{}) {}
func (r *clientLogRecorder) Info(_ context.Context, args ...interface{}) {
	r.messages = append(r.messages, fmt.Sprint(args...))
}
func (r *clientLogRecorder) Warn(context.Context, ...interface{})  {}
func (r *clientLogRecorder) Error(context.Context, ...interface{}) {}

func TestNewLarkClientLogging(t *testing.T) {
	for _, tc := range []struct {
		name      string
		verbose   bool
		wantReady bool
	}{
		{name: "quiet default"},
		{name: "explicit info override", verbose: true, wantReady: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &clientLogRecorder{}
			options := []lark.ClientOptionFunc{lark.WithLogger(recorder)}
			if tc.verbose {
				options = append(options, lark.WithLogLevel(larkcore.LogLevelInfo))
			}
			NewLarkClient("", "", options...)
			gotReady := strings.Contains(strings.Join(recorder.messages, "\n"), "client ready")
			if gotReady != tc.wantReady {
				t.Fatalf("client ready logged = %v, want %v; logs: %v", gotReady, tc.wantReady, recorder.messages)
			}
		})
	}
}
