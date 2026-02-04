// Package response provides the Response type returned by HTTP client operations.
//
// [Response] contains both the HTTP response data and metadata about the request:
//
//	resp, err := client.Get(url)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Println(resp.StatusCode)  // HTTP status code
//	fmt.Println(resp.String())    // response body as string
//	fmt.Println(resp.Bytes())     // response body as []byte
//	fmt.Println(resp.AccessTime)  // request duration
//
// # Response Body
//
// The response body is available through several methods:
//   - [Response.String] - body as string
//   - [Response.Bytes] - body as byte slice
//   - [Response.Buffer] - body as *bytes.Buffer
//   - [Response.Len] - body length
//
// # Metadata
//
// Response includes useful metadata:
//   - [Response.UniqueIdentifier] - unique request ID for tracing
//   - [Response.RequestTime] - when the request was sent
//   - [Response.ResponseTime] - when the response was received
//   - [Response.AccessTime] - total request duration
//   - [Response.Redirected] - whether a redirect occurred
//   - [Response.Location] - final URL after redirects
//
// # Error Handling
//
// The [Response.Error] field stores any error that occurred during the request.
// This is useful for batch operations where responses are collected for later
// inspection:
//
//	for _, resp := range responses {
//	    if resp.Error != nil {
//	        log.Printf("request to %s failed: %v", resp.URL, resp.Error)
//	    }
//	}
package response
