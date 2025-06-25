// Package middleware provides a net/http handler to handle CORS related requests.
// This implementation is guided by Clean Architecture principles, separating the
// core CORS logic from the HTTP transport layer.
package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	headerOrigin                        = "Origin"
	headerVary                          = "Vary"
	headerAccessControlRequestMethod    = "Access-Control-Request-Method"
	headerAccessControlRequestHeaders   = "Access-Control-Request-Headers"
	headerAccessControlAllowOrigin      = "Access-Control-Allow-Origin"
	headerAccessControlAllowMethods     = "Access-Control-Allow-Methods"
	headerAccessControlAllowHeaders     = "Access-Control-Allow-Headers"
	headerAccessControlAllowCredentials = "Access-Control-Allow-Credentials"
	headerAccessControlExposeHeaders    = "Access-Control-Expose-Headers"
	headerAccessControlMaxAge           = "Access-Control-Max-Age"
)

// wildcard represents a string with a single wildcard character ('*').
type wildcard struct {
	prefix string
	suffix string
}

// match returns true if the string s matches the wildcard pattern.
func (w wildcard) match(s string) bool {
	return len(s) >= len(w.prefix+w.suffix) && strings.HasPrefix(s, w.prefix) && strings.HasSuffix(s, w.suffix)
}

// CorsOptions is a configuration container to setup the CORS middleware.
type CorsOptions struct {
	AllowedOrigins     []string
	AllowOriginFunc    func(r *http.Request, origin string) bool
	AllowedMethods     []string
	AllowedHeaders     []string
	ExposedHeaders     []string
	AllowCredentials   bool
	MaxAge             int
	OptionsPassthrough bool
	Logger             *zap.Logger
}

type corsEngine struct {
	allowedOrigins    []string
	allowedWOrigins   []wildcard
	allowOriginFunc   func(r *http.Request, origin string) bool
	allowedMethods    map[string]struct{}
	allowedHeaders    map[string]struct{}
	exposedHeaders    string
	allowCredentials  bool
	maxAge            string
	allowedOriginsAll bool
	allowedHeadersAll bool
	logger            *zap.Logger
}

func NewCors(opts CorsOptions) *Cors {
	engine := newCorsEngine(opts)
	return &Cors{
		engine:             engine,
		optionsPassthrough: opts.OptionsPassthrough,
		logger:             engine.logger, // Share the same logger
	}
}

// newCorsEngine processes the CorsOptions and returns a configured corsEngine.
// Its Cognitive Complexity is low as it delegates processing to helper functions.
func newCorsEngine(opts CorsOptions) *corsEngine {
	logger := opts.Logger
	if logger == nil {
		// Default to a disabled logger to avoid nil checks everywhere.
		logger = zap.NewNop()
	}

	allowedOriginsAll, allowedOrigins, allowedWOrigins := processOrigins(opts)
	allowedMethods := processAllowedMethods(opts.AllowedMethods)
	allowedHeadersAll, allowedHeaders := processAllowedHeaders(opts.AllowedHeaders)

	engine := &corsEngine{
		logger:            logger,
		allowOriginFunc:   opts.AllowOriginFunc,
		allowCredentials:  opts.AllowCredentials,
		allowedOriginsAll: allowedOriginsAll,
		allowedOrigins:    allowedOrigins,
		allowedWOrigins:   allowedWOrigins,
		allowedMethods:    allowedMethods,
		allowedHeadersAll: allowedHeadersAll,
		allowedHeaders:    allowedHeaders,
	}

	// These are simple enough to not require a helper function
	if len(opts.ExposedHeaders) > 0 {
		engine.exposedHeaders = buildExposedHeaders(opts.ExposedHeaders)
	}

	if opts.MaxAge > 0 {
		engine.maxAge = strconv.Itoa(opts.MaxAge)
	}

	return engine
}

// processOrigins handles the logic for parsing and normalizing allowed origins.
func processOrigins(opts CorsOptions) (isAll bool, plains []string, wildcards []wildcard) {
	// If a custom function is provided, the lists are ignored.
	if opts.AllowOriginFunc != nil {
		return false, nil, nil
	}

	// Default behavior: allow all origins if the list is empty.
	if len(opts.AllowedOrigins) == 0 {
		return true, nil, nil
	}

	for _, origin := range opts.AllowedOrigins {
		if origin == "*" {
			// "*" acts as a global wildcard, overriding everything else.
			return true, nil, nil
		}

		origin = strings.ToLower(origin)
		if i := strings.IndexByte(origin, '*'); i >= 0 {
			w := wildcard{prefix: origin[:i], suffix: origin[i+1:]}
			wildcards = append(wildcards, w)
		} else {
			plains = append(plains, origin)
		}
	}

	return false, plains, wildcards
}

// processAllowedMethods handles the logic for parsing allowed methods.
func processAllowedMethods(methods []string) map[string]struct{} {
	if len(methods) == 0 {
		// Default to simple methods if nothing is specified.
		methods = []string{http.MethodGet, http.MethodPost, http.MethodHead}
	}

	methodSet := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		methodSet[strings.ToUpper(method)] = struct{}{}
	}
	return methodSet
}

// processAllowedHeaders handles the logic for parsing and normalizing allowed headers.
func processAllowedHeaders(headers []string) (isAll bool, set map[string]struct{}) {
	// "Origin" is always implicitly allowed.
	allHeaders := append(headers, headerOrigin)
	headerSet := make(map[string]struct{}, len(allHeaders))

	for _, header := range allHeaders {
		if header == "*" {
			// "*" acts as a global wildcard.
			return true, nil
		}
		headerSet[http.CanonicalHeaderKey(header)] = struct{}{}
	}

	return false, headerSet
}

// buildExposedHeaders creates the single string for the Access-Control-Expose-Headers header.
func buildExposedHeaders(headers []string) string {
	canonicalHeaders := make([]string, len(headers))
	for i, h := range headers {
		canonicalHeaders[i] = http.CanonicalHeaderKey(h)
	}
	return strings.Join(canonicalHeaders, ", ")
}

// Cors is the HTTP middleware. It acts as an adapter between the HTTP
// layer (`net/http`) and the framework-agnostic `corsEngine`.
type Cors struct {
	engine             *corsEngine
	optionsPassthrough bool
	logger             *zap.Logger
}

// Middleware wraps a `http.Handler` with the CORS logic.
func (c *Cors) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for preflight request
		if r.Method == http.MethodOptions && r.Header.Get(headerAccessControlRequestMethod) != "" {
			c.logger.Debug("Handling preflight request")
			c.handlePreflight(w, r)
			if c.optionsPassthrough {
				next.ServeHTTP(w, r)
			} else {
				w.WriteHeader(http.StatusOK)
			}
			return
		}

		c.logger.Debug("Handling actual request")
		c.handleActualRequest(w, r)
		next.ServeHTTP(w, r)
	})
}

// handlePreflight handles pre-flight CORS requests.
func (c *Cors) handlePreflight(w http.ResponseWriter, r *http.Request) {
	headers := w.Header()
	origin := r.Header.Get(headerOrigin)

	// Always set Vary headers
	headers.Add(headerVary, headerOrigin)
	headers.Add(headerVary, headerAccessControlRequestMethod)
	headers.Add(headerVary, headerAccessControlRequestHeaders)

	if origin == "" {
		c.logger.Debug("Preflight aborted: empty origin")
		return
	}

	if !c.engine.isOriginAllowed(r, origin) {
		c.logger.Debug("Preflight aborted: origin not allowed", zap.String("origin", origin))
		return
	}

	reqMethod := r.Header.Get(headerAccessControlRequestMethod)
	if !c.engine.isMethodAllowed(reqMethod) {
		c.logger.Debug("Preflight aborted: method not allowed", zap.String("method", reqMethod))
		return
	}

	reqHeaders := parseHeaderList(r.Header.Get(headerAccessControlRequestHeaders))
	if !c.engine.areHeadersAllowed(reqHeaders) {
		c.logger.Debug("Preflight aborted: headers not allowed", zap.Any("headers", reqHeaders))
		return
	}

	if c.engine.allowedOriginsAll {
		headers.Set(headerAccessControlAllowOrigin, "*")
	} else {
		headers.Set(headerAccessControlAllowOrigin, origin)
	}

	headers.Set(headerAccessControlAllowMethods, strings.ToUpper(reqMethod))

	if len(reqHeaders) > 0 {
		headers.Set(headerAccessControlAllowHeaders, strings.Join(reqHeaders, ", "))
	}

	if c.engine.allowCredentials {
		headers.Set(headerAccessControlAllowCredentials, "true")
	}

	if c.engine.maxAge != "" {
		headers.Set(headerAccessControlMaxAge, c.engine.maxAge)
	}
}

// handleActualRequest handles simple cross-origin requests.
func (c *Cors) handleActualRequest(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get(headerOrigin)
	headers := w.Header()

	headers.Add(headerVary, headerOrigin)

	if origin == "" {
		c.logger.Debug("Actual request: no headers added, missing origin")
		return
	}

	if !c.engine.isOriginAllowed(r, origin) {
		c.logger.Debug("Actual request: no headers added, origin not allowed", zap.String("origin", origin))
		return
	}

	if !c.engine.isMethodAllowed(r.Method) {
		c.logger.Debug("Actual request: no headers added, method not allowed", zap.String("method", r.Method))
		return
	}

	if c.engine.allowedOriginsAll {
		headers.Set(headerAccessControlAllowOrigin, "*")
	} else {
		headers.Set(headerAccessControlAllowOrigin, origin)
	}

	if c.engine.exposedHeaders != "" {
		headers.Set(headerAccessControlExposeHeaders, c.engine.exposedHeaders)
	}

	if c.engine.allowCredentials {
		headers.Set(headerAccessControlAllowCredentials, "true")
	}
}

// isOriginAllowed checks if a given origin is allowed.
func (e *corsEngine) isOriginAllowed(r *http.Request, origin string) bool {
	if e.allowOriginFunc != nil {
		return e.allowOriginFunc(r, origin)
	}
	if e.allowedOriginsAll {
		return true
	}
	origin = strings.ToLower(origin)
	for _, o := range e.allowedOrigins {
		if o == origin {
			return true
		}
	}
	for _, w := range e.allowedWOrigins {
		if w.match(origin) {
			return true
		}
	}
	return false
}

// isMethodAllowed checks if a given method is allowed.
func (e *corsEngine) isMethodAllowed(method string) bool {
	if method == http.MethodOptions {
		return true // Always allow preflight requests
	}
	_, ok := e.allowedMethods[strings.ToUpper(method)]
	return ok
}

// areHeadersAllowed checks if a given list of headers are allowed.
func (e *corsEngine) areHeadersAllowed(requestedHeaders []string) bool {
	if e.allowedHeadersAll || len(requestedHeaders) == 0 {
		return true
	}
	for _, header := range requestedHeaders {
		if _, ok := e.allowedHeaders[http.CanonicalHeaderKey(header)]; !ok {
			return false
		}
	}
	return true
}

// parseHeaderList simplifies header list parsing using standard library functions.
func parseHeaderList(headerList string) []string {
	if headerList == "" {
		return nil
	}
	parts := strings.Split(headerList, ",")
	list := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

// AllowAll is a convenience constructor for a permissive CORS configuration.
func AllowAll(log *zap.Logger) *Cors {
	return NewCors(CorsOptions{
		Logger:           log,
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{http.MethodHead, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
		MaxAge:           int((12 * time.Hour).Seconds()),
	})
}
