// Package form builds multipart/form-data request bodies.
//
// Add fields and files in the order the server expects them, then pass the
// form as a request payload:
//
//	f := form.New().
//	    Field("title", "Quarterly report").
//	    File("attachment", "/path/report.pdf").
//	    File("attachment", "/path/summary.pdf")
//
//	resp, err := client.Post(url, f)
//
// The form is encoded while it is sent, and each file is read in small pieces,
// so a large file is never held in memory. Before it sends any data, the form
// checks every file and works out its exact length, so the request has a
// Content-Length. A file that cannot be read fails the request before it is
// sent. After a 307 or 308 redirect, the form is encoded again from its files.
package form
