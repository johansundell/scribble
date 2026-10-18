package fmsodata

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)

// CreateRecords creates records in one $batch request, as a single change
// set. Each create asks for Prefer: return=minimal, so the server need not
// send the records back. It returns a *StatusError when the batch, or any
// create in it, fails.
//
// Claris documents the request format but not the response, and not whether
// a change set is atomic; the response parser accepts both a response per
// create and a single error response for the whole change set.
func (c *Client) CreateRecords(ctx context.Context, tableName string, records []map[string]interface{}) error {
	if len(records) == 0 {
		return nil
	}

	body, contentType, err := c.batchCreateBody(tableName, records)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/$batch", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("OData-Version", "4.0")

	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.handleError(resp)
	}
	return checkBatchResponse(resp.Header.Get("Content-Type"), resp.Body)
}

func (c *Client) batchCreateBody(tableName string, records []map[string]interface{}) (io.Reader, string, error) {
	batchBoundary := "batch_" + randomID()
	changesetBoundary := "changeset_" + randomID()
	createURL := fmt.Sprintf("%s/%s", c.baseURL, tableName)

	var changeset bytes.Buffer
	for i, record := range records {
		payload, err := json.Marshal(record)
		if err != nil {
			return nil, "", err
		}
		fmt.Fprintf(&changeset, "--%s\r\n", changesetBoundary)
		changeset.WriteString("Content-Type: application/http\r\n")
		changeset.WriteString("Content-Transfer-Encoding: binary\r\n")
		fmt.Fprintf(&changeset, "Content-ID: %d\r\n\r\n", i+1)
		fmt.Fprintf(&changeset, "POST %s HTTP/1.1\r\n", createURL)
		changeset.WriteString("Content-Type: application/json\r\n")
		changeset.WriteString("Prefer: return=minimal\r\n")
		fmt.Fprintf(&changeset, "Content-Length: %d\r\n\r\n", len(payload))
		changeset.Write(payload)
		changeset.WriteString("\r\n")
	}
	fmt.Fprintf(&changeset, "--%s--\r\n", changesetBoundary)

	var body bytes.Buffer
	fmt.Fprintf(&body, "--%s\r\n", batchBoundary)
	fmt.Fprintf(&body, "Content-Type: multipart/mixed; boundary=%s\r\n\r\n", changesetBoundary)
	body.Write(changeset.Bytes())
	fmt.Fprintf(&body, "--%s--\r\n", batchBoundary)

	return &body, "multipart/mixed; boundary=" + batchBoundary, nil
}

// checkBatchResponse walks a multipart/mixed batch response and returns the
// first failed inner response as a *StatusError.
func checkBatchResponse(contentType string, body io.Reader) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return fmt.Errorf("unexpected $batch response content type %q", contentType)
	}
	return checkBatchParts(multipart.NewReader(body, params["boundary"]))
}

func checkBatchParts(r *multipart.Reader) error {
	parts := 0
	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read $batch response: %w", err)
		}
		parts++

		mediaType, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if strings.HasPrefix(mediaType, "multipart/") {
			// Change set: one response per request inside it.
			if err := checkBatchParts(multipart.NewReader(part, params["boundary"])); err != nil {
				return err
			}
			continue
		}
		if err := checkInnerResponse(part); err != nil {
			return err
		}
	}
	if parts == 0 {
		return errors.New("empty $batch response")
	}
	return nil
}

func checkInnerResponse(part io.Reader) error {
	data, err := io.ReadAll(part)
	if err != nil {
		return fmt.Errorf("read $batch inner response: %w", err)
	}
	// The CRLF before a multipart boundary belongs to the boundary, so a
	// response without a body can arrive without its closing blank line.
	if !bytes.Contains(data, []byte("\r\n\r\n")) && !bytes.Contains(data, []byte("\n\n")) {
		data = append(bytes.TrimRight(data, "\r\n"), "\r\n\r\n"...)
	}
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(data)), nil)
	if err != nil {
		return fmt.Errorf("parse $batch inner response: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &StatusError{StatusCode: resp.StatusCode, Body: string(b)}
	}
	return nil
}

func randomID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
