package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
	"github.com/FortyTwoCn/cyber-amber/internal/bili/endpoints"
	"github.com/FortyTwoCn/cyber-amber/internal/store"
)

type Service struct {
	client *client.Client
	store  *store.Store
	key    []byte
}

func NewService(client *client.Client, store *store.Store, key []byte) *Service {
	return &Service{client: client, store: store, key: key}
}

type Status struct {
	LoggedIn    bool       `json:"logged_in"`
	MID         int64      `json:"mid"`
	Nickname    string     `json:"nickname"`
	LastChecked *time.Time `json:"last_checked_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

func (s *Service) Import(ctx context.Context, cookie string) error {
	if len(s.key) != 32 {
		return errors.New("未配置 COOKIE_ENCRYPTION_KEY，不能保存账号")
	}
	session, err := ParseCookieHeader(cookie)
	if err != nil {
		return err
	}
	previousHeader := s.storedCookieHeader(ctx)
	s.client.SetCookie(session.CookieHeader())
	status, err := s.Check(ctx)
	if err != nil {
		s.client.SetCookie(previousHeader)
		return err
	}
	if !status.LoggedIn {
		s.client.SetCookie(previousHeader)
		return errors.New("导入的 B站 Cookie 已被拒绝")
	}
	if err := s.save(ctx, session, status); err != nil {
		s.client.SetCookie(previousHeader)
		return err
	}
	return nil
}

func (s *Service) storedCookieHeader(ctx context.Context) string {
	if previous, err := s.Session(ctx); err == nil {
		return previous.CookieHeader()
	}
	return ""
}
func (s *Service) Load(ctx context.Context) error {
	record, err := s.store.LoadAccount(ctx)
	if err != nil {
		return err
	}
	if !record.LoggedIn {
		s.client.SetCookie("")
		return errors.New("BILI_AUTH_INVALID: 数据库中的 B站账号已标记为失效")
	}
	if len(s.key) != 32 {
		return errors.New("数据库中存在账号，但 COOKIE_ENCRYPTION_KEY 缺失")
	}
	plaintext, err := Decrypt(s.key, record.Encrypted, record.Nonce)
	if err != nil {
		return err
	}
	session, err := UnmarshalSession(plaintext)
	if err != nil {
		return err
	}
	s.client.SetCookie(session.CookieHeader())
	return nil
}

func (s *Service) Session(ctx context.Context) (Session, error) {
	record, err := s.store.LoadAccount(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return Session{}, errors.New("BILI_AUTH_INVALID: 尚未配置 B站账号")
	}
	if err != nil {
		return Session{}, err
	}
	if !record.LoggedIn {
		return Session{}, errors.New("BILI_AUTH_INVALID: B站账号已失效")
	}
	if len(s.key) != 32 {
		return Session{}, errors.New("COOKIE_ENCRYPTION_KEY 缺失，无法读取账号")
	}
	plaintext, err := Decrypt(s.key, record.Encrypted, record.Nonce)
	if err != nil {
		return Session{}, err
	}
	return UnmarshalSession(plaintext)
}
func (s *Service) Check(ctx context.Context) (Status, error) {
	var result client.Envelope[struct {
		IsLogin bool   `json:"isLogin"`
		MID     int64  `json:"mid"`
		Uname   string `json:"uname"`
	}]
	if err := s.client.GetJSON(ctx, endpoints.AccountInfo, nil, &result); err != nil {
		return Status{}, err
	}
	if result.Code != 0 && result.Code != -101 && result.Code != -111 {
		return Status{}, client.Check(result.Code, result.Message)
	}
	now := time.Now().UTC()
	status := Status{LoggedIn: result.Data.IsLogin, MID: result.Data.MID, Nickname: result.Data.Uname, LastChecked: &now}
	if !status.LoggedIn {
		status.Error = result.Message
	}
	return status, nil
}
func (s *Service) Status(ctx context.Context) (Status, error) {
	record, err := s.store.LoadAccount(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{LoggedIn: record.LoggedIn, MID: record.MID, Nickname: record.Nickname, LastChecked: record.LastChecked, Error: record.LastError}, nil
}

func (s *Service) HealthCheck(ctx context.Context) (Status, error) {
	session, err := s.Session(ctx)
	if err != nil {
		return Status{}, err
	}
	s.client.SetCookie(session.CookieHeader())
	status, err := s.Check(ctx)
	if err != nil {
		return Status{}, err
	}
	if err := s.save(ctx, session, status); err != nil {
		return Status{}, err
	}
	return status, nil
}
func (s *Service) save(ctx context.Context, session Session, status Status) error {
	plain, err := session.Marshal()
	if err != nil {
		return err
	}
	ciphertext, nonce, err := Encrypt(s.key, plain)
	if err != nil {
		return err
	}
	var refreshCipher, refreshNonce []byte
	if session.RefreshToken != "" {
		refreshCipher, refreshNonce, err = Encrypt(s.key, []byte(session.RefreshToken))
		if err != nil {
			return err
		}
	}
	return s.store.SaveAccount(ctx, store.AccountRecord{MID: status.MID, Nickname: status.Nickname, Encrypted: ciphertext, Nonce: nonce, RefreshEncrypted: refreshCipher, RefreshNonce: refreshNonce, LoggedIn: status.LoggedIn, LastChecked: status.LastChecked, LastError: status.Error})
}

type QRCode struct {
	URL string `json:"url"`
	Key string `json:"key"`
}
type QRPoll struct {
	State   string  `json:"state"`
	Message string  `json:"message"`
	Status  *Status `json:"account,omitempty"`
}

func (s *Service) GenerateQR(ctx context.Context) (QRCode, error) {
	var result client.Envelope[struct {
		URL string `json:"url"`
		Key string `json:"qrcode_key"`
	}]
	if err := s.client.GetPassportJSON(ctx, endpoints.QRCodeGenerate, nil, &result); err != nil {
		return QRCode{}, err
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return QRCode{}, err
	}
	if result.Data.URL == "" || result.Data.Key == "" {
		return QRCode{}, errors.New("二维码响应结构不兼容")
	}
	return QRCode{URL: result.Data.URL, Key: result.Data.Key}, nil
}
func (s *Service) PollQR(ctx context.Context, key string) (QRPoll, error) {
	if len(s.key) != 32 {
		return QRPoll{}, errors.New("未配置 COOKIE_ENCRYPTION_KEY，不能保存扫码账号")
	}
	if key == "" || len(key) > 128 {
		return QRPoll{}, errors.New("二维码 key 无效")
	}
	var result client.Envelope[struct {
		URL          string `json:"url"`
		RefreshToken string `json:"refresh_token"`
		Code         int    `json:"code"`
		Message      string `json:"message"`
	}]
	if err := s.client.GetPassportJSON(ctx, endpoints.QRCodePoll, url.Values{"qrcode_key": {key}}, &result); err != nil {
		return QRPoll{}, err
	}
	if err := client.Check(result.Code, result.Message); err != nil {
		return QRPoll{}, err
	}
	switch result.Data.Code {
	case 86101:
		return QRPoll{State: "waiting", Message: result.Data.Message}, nil
	case 86090:
		return QRPoll{State: "scanned", Message: result.Data.Message}, nil
	case 86038:
		return QRPoll{State: "expired", Message: result.Data.Message}, nil
	case 0:
		session, err := SessionFromLoginURL(result.Data.URL, result.Data.RefreshToken, nil)
		if err != nil {
			return QRPoll{}, err
		}
		previousHeader := s.storedCookieHeader(ctx)
		s.client.SetCookie(session.CookieHeader())
		status, err := s.Check(ctx)
		if err != nil {
			s.client.SetCookie(previousHeader)
			return QRPoll{}, err
		}
		if !status.LoggedIn {
			s.client.SetCookie(previousHeader)
			return QRPoll{}, errors.New("扫码完成但账号确认失败")
		}
		if err := s.save(ctx, session, status); err != nil {
			s.client.SetCookie(previousHeader)
			return QRPoll{}, err
		}
		return QRPoll{State: "confirmed", Message: "登录成功", Status: &status}, nil
	default:
		return QRPoll{}, fmt.Errorf("未知扫码状态 %d: %s", result.Data.Code, result.Data.Message)
	}
}
