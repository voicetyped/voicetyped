package dialog

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"text/template"
)

const maxTemplateOutput = 64 * 1024

// templateCache caches parsed templates to avoid re-parsing on every call.
var templateCache sync.Map

// templateCtx is the data available in Go template expressions.
type templateCtx struct {
	Session   *Session
	Event     any
	Variables map[string]string
	Result    map[string]any
}

func newTemplateCtx(session *Session) templateCtx {
	return templateCtx{
		Session:   session,
		Event:     session.GetLastEvent(),
		Variables: session.CopyVariables(),
		Result:    session.GetLastResult(),
	}
}

// EvalCondition evaluates a Go template condition string.
// Returns true if the result is non-empty and not "false".
func EvalCondition(condition string, session *Session) (bool, error) {
	if condition == "" {
		return true, nil
	}

	result, err := renderTemplate(condition, session)
	if err != nil {
		return false, err
	}

	result = strings.TrimSpace(result)
	return result != "" && result != "false" && result != "<no value>", nil
}

// RenderParam evaluates a Go template string in param values.
func RenderParam(tmpl string, session *Session) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}
	return renderTemplate(tmpl, session)
}

// limitWriter caps output from template.Execute.
type limitWriter struct {
	w       io.Writer
	n       int64
	written int64
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if lw.written+int64(len(p)) > lw.n {
		allowed := lw.n - lw.written
		if allowed > 0 {
			n, err := lw.w.Write(p[:allowed])
			lw.written += int64(n)
			if err != nil {
				return n, err
			}
		}
		return 0, fmt.Errorf("template output exceeds %d bytes", lw.n)
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	return n, err
}

func renderTemplate(tmplStr string, session *Session) (string, error) {
	var tmpl *template.Template
	if cached, ok := templateCache.Load(tmplStr); ok {
		tmpl = cached.(*template.Template)
	} else {
		var err error
		tmpl, err = template.New("").Parse(tmplStr)
		if err != nil {
			return "", err
		}
		templateCache.Store(tmplStr, tmpl)
	}

	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, n: maxTemplateOutput}
	if err := tmpl.Execute(lw, newTemplateCtx(session)); err != nil {
		return "", err
	}
	return buf.String(), nil
}
