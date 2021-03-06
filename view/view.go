package view

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"path/filepath"
)

var (
	LayoutDir   string = "template/layout/"
	TemplateDir string = "template/"
	TemplateExt string = ".gohtml"
)

type View struct {
	Template *template.Template
	Layout   string
}

func (v *View) Render(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-type", "text/html")

	// this is like a convience wrapper
	switch data.(type) {
	case Data:
	default:
		data = Data{
			Content: data,
		}
	}

	buf := new(bytes.Buffer)
	err := v.Template.ExecuteTemplate(buf, v.Layout, data)
	if err != nil {
		http.Error(w, "Something went wrong!", http.StatusInternalServerError)
		return
	}

	io.Copy(w, buf)
}

func (v *View) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v.Render(w, nil)
}

func layoutFiles() []string {
	files, err := filepath.Glob(LayoutDir + "*" + TemplateExt)
	if err != nil {
		panic(err)
	}
	return files
}

func addTemplatePath(files []string) {
	for i, f := range files {
		files[i] = TemplateDir + f
	}
}

func addTemplateExt(files []string) {
	for i, f := range files {
		files[i] = f + TemplateExt
	}
}

func New(layout string, files ...string) *View {
	addTemplatePath(files)
	addTemplateExt(files)
	files = append(files, layoutFiles()...)
	t, err := template.ParseFiles(files...)
	if err != nil {
		panic(err)
	}

	return &View{
		Template: t,
		Layout:   layout,
	}
}
