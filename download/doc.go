// Package download checks and stores HTTP response data. It handles buffered
// responses, including JSON and HTTP error bodies, as well as ordinary,
// resumable and segmented file downloads. Decompression, checksums and body
// limits are shared across these paths.
package download
