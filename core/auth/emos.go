package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/model"
)

type EmosError struct {
	Status  int
	Message string
}

func (e *EmosError) Error() string { return e.Message }

func (e *EmosError) Unwrap() error {
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusBadRequest {
		return model.ErrInvalidAuth
	}
	return nil
}

var emosHTTPClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

type emosProfile struct {
	UserID      string `json:"user_id"`
	EmosID      string `json:"emos_id"`
	Username    string `json:"username"`
	CanDownload *bool  `json:"is_can_down"`
	response    any
}

func AuthenticateEmos(ctx context.Context, ds model.DataStore, username, password, clientIP, userAgent string) (*model.User, error) {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return nil, &EmosError{Status: http.StatusBadRequest, Message: "用户名和密码不能为空"}
	}
	body, err := json.Marshal(map[string]string{
		"username":  username,
		"password":  password,
		"client_ip": clientIP,
		"ua":        userAgent,
	})
	if err != nil {
		return nil, err
	}
	profile, err := fetchEmosProfile(ctx, http.MethodPost, "/server/music/userLogin", body, "")
	if err != nil {
		return nil, err
	}
	if profile.CanDownload == nil {
		return nil, invalidEmosProfile(profile.response)
	}
	return syncEmosProfile(ctx, ds, profile, profile.EmosID, profile.CanDownload, &password)
}

func AuthenticateEmosToken(ctx context.Context, ds model.DataStore, token string) (*model.User, error) {
	if strings.TrimSpace(token) == "" || len(token) > 4096 {
		return nil, &EmosError{Status: http.StatusUnauthorized, Message: "登录已失效，请重新登录"}
	}
	profile, err := fetchEmosProfile(ctx, http.MethodGet, "/api/user/base", nil, token)
	if err != nil {
		return nil, err
	}
	return syncEmosProfile(ctx, ds, profile, profile.UserID, nil, nil)
}

func syncEmosProfile(ctx context.Context, ds model.DataStore, profile *emosProfile, emosID string, canDownload *bool, password *string) (*model.User, error) {
	if strings.TrimSpace(emosID) == "" || utf8.RuneCountInString(emosID) > 10 || strings.TrimSpace(profile.Username) == "" || len(profile.Username) > 255 {
		return nil, invalidEmosProfile(profile.response)
	}
	return ds.User().SyncEmosUser(ctx, emosID, profile.Username, canDownload, password)
}

func fetchEmosProfile(ctx context.Context, method, endpoint string, body []byte, token string) (*emosProfile, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(conf.Server.EmosURL), "/")
	if baseURL == "" {
		return nil, &EmosError{Status: http.StatusServiceUnavailable, Message: "EMOS 登录尚未配置"}
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &EmosError{Status: http.StatusServiceUnavailable, Message: "EMOS 登录配置无效"}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := emosHTTPClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, &EmosError{Status: http.StatusServiceUnavailable, Message: "暂时无法连接 EMOS，请稍后重试"}
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return nil, &EmosError{Status: http.StatusBadGateway, Message: "读取 EMOS 响应失败，请稍后重试"}
	}
	var response any
	if err := json.Unmarshal(responseBody, &response); err != nil {
		response = strings.TrimSpace(string(responseBody))
	}

	if res.StatusCode != http.StatusOK {
		status := res.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		message := emosResponseMessage(response)
		if message == "" {
			message = fmt.Sprintf("EMOS 请求失败（HTTP %d）", res.StatusCode)
		}
		return nil, &EmosError{Status: status, Message: message}
	}
	var profile emosProfile
	if err := json.Unmarshal(responseBody, &profile); err != nil {
		return nil, invalidEmosProfile(response)
	}
	profile.response = response
	return &profile, nil
}

func emosResponseMessage(response any) string {
	if body, ok := response.(map[string]any); ok {
		for _, field := range []string{"message", "error"} {
			if message, ok := body[field].(string); ok && strings.TrimSpace(message) != "" {
				return message
			}
		}
	}
	if message, ok := response.(string); ok {
		return strings.TrimSpace(message)
	}
	if response == nil {
		return ""
	}
	message, _ := json.Marshal(response)
	return string(message)
}

func invalidEmosProfile(response any) *EmosError {
	message := "EMOS 用户信息无效"
	if detail := emosResponseMessage(response); detail != "" {
		message += "：" + detail
	}
	return &EmosError{Status: http.StatusBadGateway, Message: message}
}
