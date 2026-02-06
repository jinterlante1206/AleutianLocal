// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package telemetry

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// ExtractContext extracts trace context from incoming HTTP headers.
//
// Description:
//
//	Uses the globally configured propagator (set in Init) to extract
//	W3C TraceContext and Baggage from HTTP headers. The returned context
//	contains the extracted trace information and can be used to create
//	child spans.
//
// Inputs:
//
//	ctx - Base context to extend with trace information.
//	headers - HTTP headers containing trace context (e.g., traceparent, tracestate).
//
// Outputs:
//
//	context.Context - Context with trace information attached.
//	               Returns the original context if no trace headers are present.
//
// Example:
//
//	func handleRequest(w http.ResponseWriter, r *http.Request) {
//	    ctx := telemetry.ExtractContext(r.Context(), r.Header)
//	    ctx, span := tracer.Start(ctx, "handleRequest")
//	    defer span.End()
//	    // ... handle request
//	}
//
// Thread Safety: Safe for concurrent use.
func ExtractContext(ctx context.Context, headers http.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
}

// InjectContext injects trace context into outgoing HTTP headers.
//
// Description:
//
//	Uses the globally configured propagator (set in Init) to inject
//	W3C TraceContext and Baggage into HTTP headers. Use this when
//	making outgoing HTTP requests to propagate trace context.
//
// Inputs:
//
//	ctx - Context containing active span information.
//	headers - HTTP headers to inject trace context into.
//
// Example:
//
//	func callDownstream(ctx context.Context, url string) error {
//	    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
//	    telemetry.InjectContext(ctx, req.Header)
//	    resp, err := http.DefaultClient.Do(req)
//	    // ...
//	}
//
// Thread Safety: Safe for concurrent use.
func InjectContext(ctx context.Context, headers http.Header) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(headers))
}

// PropagateToRequest injects trace context into an outgoing HTTP request.
//
// Description:
//
//	Convenience wrapper that extracts the context from the request,
//	injects trace context into the request headers, and returns
//	the request with updated context.
//
// Inputs:
//
//	ctx - Context containing active span information.
//	req - HTTP request to inject trace context into.
//
// Outputs:
//
//	*http.Request - Request with context and trace headers updated.
//
// Example:
//
//	func callAPI(ctx context.Context, url string) error {
//	    req, _ := http.NewRequest("GET", url, nil)
//	    req = telemetry.PropagateToRequest(ctx, req)
//	    resp, err := client.Do(req)
//	    // ...
//	}
//
// Thread Safety: Safe for concurrent use.
func PropagateToRequest(ctx context.Context, req *http.Request) *http.Request {
	InjectContext(ctx, req.Header)
	return req.WithContext(ctx)
}

// ExtractFromRequest extracts trace context from an incoming HTTP request.
//
// Description:
//
//	Convenience wrapper that extracts trace context from the request
//	headers and returns an updated context.
//
// Inputs:
//
//	req - HTTP request containing trace headers.
//
// Outputs:
//
//	context.Context - Context with trace information attached.
//
// Example:
//
//	func handleIncoming(w http.ResponseWriter, r *http.Request) {
//	    ctx := telemetry.ExtractFromRequest(r)
//	    // Use ctx for creating child spans
//	}
//
// Thread Safety: Safe for concurrent use.
func ExtractFromRequest(req *http.Request) context.Context {
	return ExtractContext(req.Context(), req.Header)
}

// MapCarrier implements propagation.TextMapCarrier for map[string]string.
//
// Description:
//
//	Allows trace context propagation with simple string maps,
//	useful for non-HTTP transports like message queues or gRPC metadata.
type MapCarrier map[string]string

// Get returns the value for a key.
func (c MapCarrier) Get(key string) string {
	return c[key]
}

// Set sets a key-value pair.
func (c MapCarrier) Set(key, value string) {
	c[key] = value
}

// Keys returns all keys in the carrier.
func (c MapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// ExtractFromMap extracts trace context from a string map.
//
// Description:
//
//	Useful for extracting trace context from non-HTTP transports
//	like message queues, gRPC metadata, or custom headers.
//
// Inputs:
//
//	ctx - Base context to extend.
//	carrier - Map containing trace context keys.
//
// Outputs:
//
//	context.Context - Context with trace information attached.
//
// Example:
//
//	func handleMessage(ctx context.Context, msg *Message) {
//	    ctx = telemetry.ExtractFromMap(ctx, msg.Headers)
//	    ctx, span := tracer.Start(ctx, "handleMessage")
//	    defer span.End()
//	}
//
// Thread Safety: Safe for concurrent use.
func ExtractFromMap(ctx context.Context, carrier map[string]string) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, MapCarrier(carrier))
}

// InjectToMap injects trace context into a string map.
//
// Description:
//
//	Useful for propagating trace context through non-HTTP transports.
//	If carrier is nil, a new map is created and returned.
//
// Inputs:
//
//	ctx - Context containing active span information.
//	carrier - Map to inject trace context into. May be nil.
//
// Outputs:
//
//	map[string]string - Map with trace context injected.
//
// Example:
//
//	func publishMessage(ctx context.Context, payload []byte) error {
//	    headers := telemetry.InjectToMap(ctx, nil)
//	    msg := &Message{Payload: payload, Headers: headers}
//	    return queue.Publish(msg)
//	}
//
// Thread Safety: Safe for concurrent use.
func InjectToMap(ctx context.Context, carrier map[string]string) map[string]string {
	if carrier == nil {
		carrier = make(map[string]string)
	}
	otel.GetTextMapPropagator().Inject(ctx, MapCarrier(carrier))
	return carrier
}
