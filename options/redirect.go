package options

// RedirectConfig holds redirect behaviour settings.
type RedirectConfig struct {
	// Follow determines whether HTTP redirects are automatically followed.
	// When false (default), the redirect response is returned as-is.
	Follow bool

	// PreserveMethod maintains the original HTTP method on redirect.
	// By default (false), redirects switch to GET as per HTTP spec.
	PreserveMethod bool

	// Max is the maximum number of redirects to follow before giving up.
	Max int
}

// defaultRedirectConfig returns the default redirect configuration.
func defaultRedirectConfig() RedirectConfig {
	return RedirectConfig{
		Follow:         false,
		PreserveMethod: false,
		Max:            10,
	}
}

// Redirects configures HTTP redirect behavior.
// enabled - whether to follow redirects
// preserve - whether to preserve the original HTTP method on redirect
// max - maximum number of redirects to follow (defaults to 5 if 0)
func (opt *Option) Redirects(enabled bool, preserve bool, max int) *Option {
	if max == 0 {
		max = 5
	}
	opt.mu.Lock()
	opt.Redirect.Follow = enabled
	opt.Redirect.PreserveMethod = preserve
	opt.Redirect.Max = max
	opt.mu.Unlock()
	return opt
}

// EnableRedirects configures the Option to follow HTTP redirects.
func (opt *Option) EnableRedirects() *Option {
	opt.mu.Lock()
	opt.Redirect.Follow = true
	opt.mu.Unlock()
	return opt
}

// DisableRedirects configures the Option to not follow HTTP redirects.
func (opt *Option) DisableRedirects() *Option {
	opt.mu.Lock()
	opt.Redirect.Follow = false
	opt.mu.Unlock()
	return opt
}

// EnablePreserveMethod configures redirects to maintain the original HTTP method.
func (opt *Option) EnablePreserveMethod() *Option {
	opt.mu.Lock()
	opt.Redirect.PreserveMethod = true
	opt.mu.Unlock()
	return opt
}

// DisablePreserveMethod configures redirects to not maintain the original HTTP method.
func (opt *Option) DisablePreserveMethod() *Option {
	opt.mu.Lock()
	opt.Redirect.PreserveMethod = false
	opt.mu.Unlock()
	return opt
}

// SetMaxRedirects sets the maximum number of redirects to follow.
func (opt *Option) SetMaxRedirects(max int) *Option {
	opt.mu.Lock()
	opt.Redirect.Max = max
	opt.mu.Unlock()
	return opt
}

// MaxRedirects returns the maximum number of redirects configured.
func (opt *Option) MaxRedirects() int {
	opt.mu.RLock()
	max := opt.Redirect.Max
	opt.mu.RUnlock()
	return max
}
