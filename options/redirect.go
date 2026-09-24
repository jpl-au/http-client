package options

// RedirectConfig holds redirect behaviour settings.
type RedirectConfig struct {
	// Follow determines whether HTTP redirects are automatically followed.
	// When false (default), the redirect response is returned as-is.
	// Redirects follow net/http rules: 307 and 308 repeat the method and body,
	// and 301, 302 and 303 change a POST to a GET without a body.
	Follow bool

	// Max is the maximum number of redirects to follow before giving up.
	Max int
}

// defaultRedirectConfig returns the default redirect configuration.
func defaultRedirectConfig() RedirectConfig {
	return RedirectConfig{
		Follow: false,
		Max:    10,
	}
}

// Redirects configures HTTP redirect behavior.
// enabled - whether to follow redirects
// max - maximum number of redirects to follow (the default of 10 if 0)
func (opt *Option) Redirects(enabled bool, max int) *Option {
	if max == 0 {
		max = defaultRedirectConfig().Max
	}
	opt.mu.Lock()
	opt.Redirect.Follow = enabled
	opt.Redirect.Max = max
	opt.explicit |= settingFollow | settingMaxRedirects
	opt.mu.Unlock()
	return opt
}

// EnableRedirects configures the Option to follow HTTP redirects.
func (opt *Option) EnableRedirects() *Option {
	opt.mu.Lock()
	opt.Redirect.Follow = true
	opt.explicit |= settingFollow
	opt.mu.Unlock()
	return opt
}

// DisableRedirects configures the Option to not follow HTTP redirects.
func (opt *Option) DisableRedirects() *Option {
	opt.mu.Lock()
	opt.Redirect.Follow = false
	opt.explicit |= settingFollow
	opt.mu.Unlock()
	return opt
}

// SetMaxRedirects sets the maximum number of redirects to follow.
func (opt *Option) SetMaxRedirects(max int) *Option {
	opt.mu.Lock()
	opt.Redirect.Max = max
	opt.explicit |= settingMaxRedirects
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
