// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/label.go.
package adapter

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
)

// fetchLabelFromURL executes an already-constructed HTTP request, reads the
// response body, and returns it as a base64-encoded LabelResponse.
// Used by adapters that fetch labels from a URL rather than receiving them
// inline in the booking response.
func fetchLabelFromURL(
	_ context.Context,
	client *http.Client,
	req *http.Request,
	labelReq LabelRequest,
	carrier string,
) (*LabelResponse, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("label request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return nil, fmt.Errorf("label API returned status %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read label response: %w", err)
	}

	return &LabelResponse{
		TrackingNumber: labelReq.TrackingNumber,
		Carrier:        carrier,
		Format:         labelReq.Format,
		Data:           base64.StdEncoding.EncodeToString(data),
		MimeType:       MimeTypeForFormat(labelReq.Format),
	}, nil
}

// unsupportedFormat returns a descriptive error for carriers that do not
// support the requested label format.
func unsupportedFormat(carrier string, format LabelFormat, supported ...LabelFormat) error {
	return fmt.Errorf(
		"%s does not support %s labels; supported formats: %v",
		carrier, format, supported,
	)
}
