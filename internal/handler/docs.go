// Package handler provides the HTTP handler for built-in API documentation.
// This file is located at /internal/handler/docs.go.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/docs"
)

// docsRegistry is the shared documentation registry built once at init time.
// Handlers access it via the package-level variable to avoid per-request
// allocations; the registry is read-only after construction.
var docsRegistry = docs.New()

// DocsIndex handles GET /docs.
// Returns a full documentation index: every endpoint with its summary and
// path, followed by the complete freight terminology glossary.
// Responds with JSON by default; plain text when Accept: text/plain is set.
func (c *Config) DocsIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	if acceptsPlainText(r) {
		c.docsIndexText(w, r)
		return
	}

	type endpointSummary struct {
		Slug    string `json:"slug"`
		Method  string `json:"method"`
		Path    string `json:"path"`
		Summary string `json:"summary"`
		DocsURL string `json:"docsUrl"`
	}

	endpoints := docsRegistry.Endpoints()
	summaries := make([]endpointSummary, len(endpoints))
	for i, e := range endpoints {
		summaries[i] = endpointSummary{
			Slug:    e.Slug,
			Method:  e.Method,
			Path:    e.Path,
			Summary: e.Summary,
			DocsURL: fmt.Sprintf("/docs/%s", e.Slug),
		}
	}

	resp := struct {
		Endpoints   []endpointSummary `json:"endpoints"`
		Terminology []*docs.Term      `json:"terminology"`
		Hint        string            `json:"hint"`
	}{
		Endpoints:   summaries,
		Terminology: docsRegistry.Terms(),
		Hint:        "GET /docs/{slug} for full details and example payload. GET /docs/terminology for the glossary only.",
	}

	writeJSON(w, r, c, http.StatusOK, resp)
}

// DocsEndpoint handles GET /docs/{slug}.
// Returns full documentation for the named endpoint, including description,
// field list, example JSON payload, and the corresponding curl invocation.
// The slug "terminology" is reserved and returns the glossary.
func (c *Config) DocsEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	slug := mux.Vars(r)["slug"]

	// Reserved slug: return the full terminology glossary.
	if slug == "terminology" {
		if acceptsPlainText(r) {
			c.terminologyText(w, r)
			return
		}
		writeJSON(w, r, c, http.StatusOK, struct {
			Terminology []*docs.Term `json:"terminology"`
		}{Terminology: docsRegistry.Terms()})
		return
	}

	ep := docsRegistry.Endpoint(slug)
	if ep == nil {
		c.writeError(w, r, http.StatusNotFound, "endpoint not found",
			fmt.Sprintf("%q is not a known endpoint slug — GET /docs for the full list", slug))
		return
	}

	if acceptsPlainText(r) {
		c.docsEndpointText(w, r, ep)
		return
	}

	writeJSON(w, r, c, http.StatusOK, ep)
}

// docsIndexText writes a compact plain-text index suitable for terminal use.
func (c *Config) docsIndexText(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder

	b.WriteString("carrier-gateway API\n")
	b.WriteString(strings.Repeat("─", 60) + "\n\n")
	b.WriteString("ENDPOINTS\n\n")

	for _, e := range docsRegistry.Endpoints() {
		fmt.Fprintf(&b, "  %-8s %-40s %s\n", e.Method, e.Path, e.Summary)
		fmt.Fprintf(&b, "           GET /docs/%-28s full docs + example\n\n", e.Slug)
	}

	b.WriteString("TERMINOLOGY\n\n")
	for _, t := range docsRegistry.Terms() {
		fmt.Fprintf(&b, "  %-22s %s\n", t.Name, t.Short)
	}
	b.WriteString("\nGET /docs/terminology  for extended term descriptions\n")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, b.String()); err != nil {
		c.loggerFor(r).Error("failed to write docs index", zap.Error(err))
	}
}

// docsEndpointText writes plain-text documentation for a single endpoint.
func (c *Config) docsEndpointText(w http.ResponseWriter, r *http.Request, ep *docs.Endpoint) {
	var b strings.Builder

	fmt.Fprintf(&b, "%s %s\n", ep.Method, ep.Path)
	b.WriteString(strings.Repeat("─", 60) + "\n\n")
	fmt.Fprintf(&b, "%s\n\n", ep.Description)

	if len(ep.Fields) > 0 {
		b.WriteString("FIELDS\n\n")
		for _, f := range ep.Fields {
			req := "optional"
			if f.Required {
				req = "required"
			}
			fmt.Fprintf(&b, "  %-30s %-8s %-8s %s\n", f.Name, f.Type, req, f.Description)
		}
		b.WriteString("\n")
	}

	if ep.Payload != "" {
		b.WriteString("EXAMPLE PAYLOAD\n\n")
		b.WriteString(ep.Payload)
		b.WriteString("\n\n")
	}

	if ep.Curl != "" {
		b.WriteString("CURL\n\n")
		b.WriteString(ep.Curl)
		b.WriteString("\n")
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, b.String()); err != nil {
		c.loggerFor(r).Error("failed to write endpoint docs", zap.Error(err))
	}
}

// terminologyText writes the freight glossary in curl-help style.
func (c *Config) terminologyText(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder

	b.WriteString("Freight Terminology\n")
	b.WriteString(strings.Repeat("─", 60) + "\n\n")

	for _, t := range docsRegistry.Terms() {
		fmt.Fprintf(&b, "  %-22s %s\n", t.Name, t.Short)
		if t.Description != "" {
			// Indent each line of the extended description by 4 spaces.
			for _, line := range strings.Split(t.Description, "\n") {
				fmt.Fprintf(&b, "    %s\n", strings.TrimSpace(line))
			}
		}
		b.WriteString("\n")
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, b.String()); err != nil {
		c.loggerFor(r).Error("failed to write terminology", zap.Error(err))
	}
}

// acceptsPlainText reports whether the request prefers a plain-text response.
func acceptsPlainText(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/plain")
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, r *http.Request, c *Config, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		c.loggerFor(r).Error("failed to write JSON response", zap.Error(err))
	}
}
