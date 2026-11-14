package server

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/deluan/rest"
	"github.com/go-chi/jwtauth/v5"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/utils/gravatar"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	ErrNoUsers         = errors.New("no users created")
	ErrUnauthenticated = errors.New("request not authenticated")
)

func login(ds model.DataStore) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, err := getCredentialsFromBody(r)
		if err != nil {
			log.Error(r, "Parsing request body", err)
			_ = rest.RespondWithError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}

		doLogin(ds, username, password, w, r)
	}
}

func doLogin(ds model.DataStore, username string, password string, w http.ResponseWriter, r *http.Request) {
	user, err := auth.AuthenticateEmos(r.Context(), ds, username, password, ClientIP(r), r.UserAgent())
	if err != nil {
		respondLoginError(w, r, err)
		return
	}
	respondLogin(w, r, user)
}

func respondLogin(w http.ResponseWriter, r *http.Request, user *model.User) {
	tokenString, err := auth.CreateToken(user)
	if err != nil {
		_ = rest.RespondWithError(w, http.StatusInternalServerError, "Unknown error authenticating user. Please try again")
		return
	}
	payload := buildAuthPayload(user)
	payload["token"] = tokenString
	_ = rest.RespondWithJSON(w, http.StatusOK, payload)
}

func respondLoginError(w http.ResponseWriter, r *http.Request, err error) {
	var emosErr *auth.EmosError
	if errors.As(err, &emosErr) {
		_ = rest.RespondWithError(w, emosErr.Status, emosErr.Message)
		return
	}
	log.Error(r, "Could not load EMOS account", err)
	_ = rest.RespondWithError(w, http.StatusInternalServerError, "无法读取用户信息，请稍后重试")
}

func emosTokenLogin(ds model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var data struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			_ = rest.RespondWithError(w, http.StatusBadRequest, "缺少登录凭证")
			return
		}
		user, err := auth.AuthenticateEmosToken(r.Context(), ds, data.Token)
		if err != nil {
			respondLoginError(w, r, err)
			return
		}
		respondLogin(w, r, user)
	}
}

func buildAuthPayload(user *model.User) map[string]any {
	payload := map[string]any{
		"id":          user.ID,
		"name":        user.Name,
		"username":    user.UserName,
		"isAdmin":     user.IsAdmin,
		"canDownload": user.CanDownload,
	}
	// Native clients such as Feishin use these credentials for Subsonic requests.
	if user.Password != "" {
		salt := id.NewRandom()
		payload["subsonicSalt"] = salt
		payload["subsonicToken"] = fmt.Sprintf("%x", md5.Sum([]byte(user.Password+salt)))
	}
	if conf.Server.EnableGravatar && user.Email != "" {
		payload["avatar"] = gravatar.Url(user.Email, 50)
	}

	return payload
}

// MaxLoginBodySize bounds the payload of unauthenticated login routes across all APIs.
const MaxLoginBodySize = 8 << 10

func LimitLoginBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxLoginBodySize)
		next.ServeHTTP(w, r)
	})
}

func getCredentialsFromBody(r *http.Request) (username string, password string, err error) {
	data := make(map[string]string)
	decoder := json.NewDecoder(r.Body)
	if err = decoder.Decode(&data); err != nil {
		log.Error(r, "parsing request body", err)
		err = errors.New("invalid request payload")
		return
	}
	username = data["username"]
	password = data["password"]
	return username, password, nil
}

func createAdmin(ds model.DataStore) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = rest.RespondWithError(w, http.StatusForbidden, "请使用 EMOS 账号登录")
	}
}

func createAdminUser(ctx context.Context, ds model.DataStore, username, password string) error {
	log.Warn(ctx, "Creating initial user", "user", username)
	caser := cases.Title(language.Und)
	initialUser := model.User{
		ID:          id.NewRandom(),
		UserName:    username,
		Name:        caser.String(username),
		Email:       "",
		NewPassword: password,
		IsAdmin:     true,
		LastLoginAt: new(time.Now()),
	}
	err := ds.User().Put(ctx, &initialUser)
	if err != nil {
		log.Error(ctx, "Could not create initial user", "user", initialUser.UserName, err)
		return fmt.Errorf("creating initial user: %w", err)
	}
	return nil
}

func JWTVerifier(next http.Handler) http.Handler {
	return jwtauth.Verify(auth.TokenAuth, tokenFromHeader, jwtauth.TokenFromCookie, jwtauth.TokenFromQuery)(next)
}

func tokenFromHeader(r *http.Request) string {
	// Get token from authorization header.
	bearer := r.Header.Get(consts.UIAuthorizationHeader)
	if len(bearer) > 7 && strings.ToUpper(bearer[0:6]) == "BEARER" {
		return bearer[7:]
	}
	return ""
}

func UsernameFromToken(r *http.Request) string {
	token, _, err := jwtauth.FromContext(r.Context())
	if err != nil || token == nil {
		return ""
	}
	sub, _ := token.Subject()
	if sub == "" {
		return ""
	}
	log.Trace(r, "Found username in JWT token", "username", sub)
	return sub
}

func UsernameFromExtAuthHeader(r *http.Request) string {
	if conf.Server.ExtAuth.TrustedSources == "" {
		return ""
	}
	reverseProxyIp, ok := request.ReverseProxyIpFrom(r.Context())
	if !ok {
		log.Error("ExtAuth enabled but no proxy IP found in request context. Please report this error.")
		return ""
	}
	username := r.Header.Get(conf.Server.ExtAuth.UserHeader)
	if username == "" {
		return ""
	}
	if !validateIPAgainstList(reverseProxyIp, conf.Server.ExtAuth.TrustedSources) {
		log.Warn(r.Context(), "IP is not whitelisted for external authentication", "proxy-ip", reverseProxyIp, "client-ip", r.RemoteAddr)
		return ""
	}
	log.Trace(r, "Found username in ExtAuth.UserHeader", "username", username)
	return username
}

func InternalAuth(r *http.Request) string {
	username, ok := request.InternalAuthFrom(r.Context())
	if !ok {
		return ""
	}
	log.Trace(r, "Found username in InternalAuth", "username", username)
	return username
}

func UsernameFromConfig(*http.Request) string {
	return conf.Server.DevAutoLoginUsername
}

func contextWithUser(ctx context.Context, ds model.DataStore, username string) (context.Context, error) {
	user, err := ds.User().FindByUsername(ctx, username)
	if err == nil {
		ctx = log.NewContext(ctx, "username", username)
		ctx = request.WithUsername(ctx, user.UserName)
		return request.WithUser(ctx, *user), nil
	}
	log.Error(ctx, "Authenticated username not found in DB", "username", username)
	return ctx, err
}

func authenticateRequest(ds model.DataStore, r *http.Request, findUsernameFns ...func(r *http.Request) string) (context.Context, error) {
	var username string
	for _, fn := range findUsernameFns {
		username = fn(r)
		if username != "" {
			break
		}
	}
	if username == "" {
		return nil, ErrUnauthenticated
	}

	return contextWithUser(r.Context(), ds, username)
}

func Authenticator(ds model.DataStore) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, err := authenticateRequest(ds, r, UsernameFromConfig, UsernameFromToken, UsernameFromExtAuthHeader)
			if err != nil || !tokenAllowed(ctx) {
				_ = rest.RespondWithError(w, http.StatusUnauthorized, "Not authenticated")
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// tokenAllowed re-checks a JWT that actually identifies the resolved user. Header and
// config auth carry no token, so they short-circuit to true.
func tokenAllowed(ctx context.Context) bool {
	token, _, err := jwtauth.FromContext(ctx)
	if err != nil || token == nil {
		return true
	}
	usr, ok := request.UserFrom(ctx)
	if !ok {
		return true
	}
	claims := auth.ClaimsFromToken(token)
	if !strings.EqualFold(claims.Subject, usr.UserName) {
		return true
	}
	if err := auth.CheckClaims(claims, usr, auth.AudienceNative); err != nil {
		log.Warn(ctx, "Native API: rejected token", "user", claims.Subject, err)
		return false
	}
	return true
}

// refreshingWriter defers the refreshed-token header until the handler's first write, so an
// epoch the handler bumped reaches the token the client stores.
type refreshingWriter struct {
	http.ResponseWriter
	ctx   context.Context //nolint:containedctx // ResponseWriter wrapper defers work to Write, which has no ctx
	token jwt.Token
	once  sync.Once
}

func (w *refreshingWriter) setToken() {
	w.once.Do(func() {
		claims := auth.ClaimsFromToken(w.token)
		if epoch, ok := request.TokenEpochFrom(w.ctx); ok {
			claims.Epoch = epoch
		}
		newToken, err := auth.TouchClaims(claims)
		if err != nil {
			log.Error(w.ctx, "Could not sign new token", err)
			return
		}
		w.Header().Set(consts.UIAuthorizationHeader, newToken)
	})
}

func (w *refreshingWriter) WriteHeader(code int) {
	w.setToken()
	w.ResponseWriter.WriteHeader(code)
}

func (w *refreshingWriter) Write(b []byte) (int, error) {
	w.setToken()
	return w.ResponseWriter.Write(b)
}

// Flush keeps the SSE events route working through the wrap.
func (w *refreshingWriter) Flush() {
	w.setToken()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets capability lookups, such as SSE's write deadline, see past this wrap.
func (w *refreshingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// JWTRefresher updates the expiry date of the received JWT token, and adds the new one to
// the Authorization Header.
func JWTRefresher(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _, err := jwtauth.FromContext(r.Context())
		if err != nil || token == nil {
			next.ServeHTTP(w, r)
			return
		}
		ctx := request.WithTokenEpochHolder(r.Context())
		rw := &refreshingWriter{ResponseWriter: w, ctx: ctx, token: token}
		next.ServeHTTP(rw, r.WithContext(ctx))
		rw.setToken()
	})
}

func validateIPAgainstList(ip string, comaSeparatedList string) bool {
	if comaSeparatedList == "" || ip == "" {
		return false
	}

	cidrs := strings.Split(comaSeparatedList, ",")

	// Per https://github.com/golang/go/issues/49825, the remote address
	// on a unix socket is '@'
	if ip == "@" && strings.HasPrefix(conf.Server.Address, "unix:") {
		return slices.Contains(cidrs, "@")
	}

	if net.ParseIP(ip) == nil {
		ip, _, _ = net.SplitHostPort(ip)
	}

	if ip == "" {
		return false
	}

	testedIP, _, err := net.ParseCIDR(fmt.Sprintf("%s/32", ip))
	if err != nil {
		return false
	}

	for _, cidr := range cidrs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil && ipnet.Contains(testedIP) {
			return true
		}
	}

	return false
}
