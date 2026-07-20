package webui

import (
	"context"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/functions"
)

// TestJSSyntax parses each embedded JS asset with the project's QuickJS engine.
// Each file is placed inside an uncalled wrapper function (QuickJS eagerly
// parses nested function bodies, so syntax errors surface) alongside a no-op
// handler the engine calls instead of the file's IIFE. This is our stand-in for
// `node --check`, which is not available in this environment.
func TestJSSyntax(t *testing.T) {
	eng := functions.NewEngine(nil, 10*time.Second, 2)
	for _, f := range []string{"surface.js", "auth.js", "dashboard.js", "chat.js"} {
		src, err := File(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		wrapped := "function __syntax_check(){\n" + string(src) + "\n}\nfunction handler(){ return 0; }\n"
		res := eng.Execute(context.Background(), wrapped, nil)
		if res.Error != "" {
			t.Errorf("%s: JS parse error: %s", f, res.Error)
		}
	}
}
