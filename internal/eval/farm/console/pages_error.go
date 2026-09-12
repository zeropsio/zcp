package console

import (
	"errors"
	"net/http"
)

type errorPageData struct {
	Meta        pageMeta
	Title       string
	Message     string
	RetryHref   string
	ParentLabel string
	ParentHref  string
	Technical   *errorTechnicalView
}

type errorTechnicalView struct {
	Status    int
	Route     string
	Operation string
}

func renderHTMLPageError(w http.ResponseWriter, data errorPageData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	renderPage(w, "error", data)
}

func (s *Server) renderRunError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrRunNotFound) {
		s.renderNotFound(w, r, "Run not found", "This run is no longer available.", "Back to overview", "/")
		return
	}
	s.renderStoreError(w, r, http.StatusBadGateway, "Run data unavailable", "The run evidence could not be read.", "load run", err)
}

func (s *Server) renderBatchError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrBatchNotFound) {
		s.renderNotFound(w, r, "Batch not found", "This batch is no longer available.", "Back to overview", "/")
		return
	}
	s.renderStoreError(w, r, http.StatusBadGateway, "Batch data unavailable", "The batch evidence could not be read.", "load batch", err)
}

func (s *Server) renderNotFound(w http.ResponseWriter, r *http.Request, title, message, parentLabel, parentHref string) {
	renderHTMLPageError(w, errorPageData{
		Meta: s.pageMeta(r, title, "", false), Title: title, Message: message,
		ParentLabel: parentLabel, ParentHref: parentHref,
	}, http.StatusNotFound)
}

func (s *Server) renderStoreError(w http.ResponseWriter, r *http.Request, status int, title, message, operation string, cause error) {
	s.logf("html error method=%s path=%q status=%d operation=%q: %v", r.Method, r.URL.Path, status, operation, cause)
	renderHTMLPageError(w, errorPageData{
		Meta: s.pageMeta(r, title, "", false), Title: title, Message: message,
		RetryHref: r.URL.RequestURI(), ParentLabel: "Back to overview", ParentHref: "/",
		Technical: &errorTechnicalView{Status: status, Route: r.URL.Path, Operation: operation},
	}, status)
}
