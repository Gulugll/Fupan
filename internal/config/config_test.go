package config

import (
	"reflect"
	"testing"
)

var oidcEnv = []string{
	"FUPAN_NO_AUTH",
	"FUPAN_ADDR",
	"FUPAN_OIDC_ISSUER",
	"FUPAN_OIDC_CLIENT_ID",
	"FUPAN_OIDC_CLIENT_SECRET",
	"FUPAN_REDIRECT_URL",
	"FUPAN_ADMIN_SUB",
	"FUPAN_ALLOWED_SUBS",
	"FUPAN_ALLOWED_ENDPOINT_HOSTS",
}

func clearOIDCEnv(t *testing.T) {
	t.Helper()
	for _, key := range oidcEnv {
		t.Setenv(key, "")
	}
}

func setValidOIDCEnv(t *testing.T) {
	t.Helper()
	clearOIDCEnv(t)
	t.Setenv("FUPAN_NO_AUTH", "false")
	t.Setenv("FUPAN_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("FUPAN_OIDC_CLIENT_ID", "fupan-client")
	t.Setenv("FUPAN_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("FUPAN_REDIRECT_URL", "https://fupan.example.com/auth/callback")
	t.Setenv("FUPAN_ADMIN_SUB", "admin-sub")
	t.Setenv("FUPAN_ALLOWED_SUBS", " user-sub, admin-sub ,other-sub ")
}

func TestLoadNoAuthDoesNotRequireOIDC(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("FUPAN_NO_AUTH", "true")
	t.Setenv("FUPAN_ADDR", "127.0.0.1:8080")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("NoAuth 不应要求 OIDC 配置: %v", err)
	}
	if !cfg.NoAuth {
		t.Fatal("NoAuth 配置未生效")
	}
}

func TestLoadDefaultsToStandaloneMode(t *testing.T) {
	clearOIDCEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("独立部署默认配置应可直接启动: %v", err)
	}
	if !cfg.NoAuth {
		t.Fatal("独立部署默认不使用登录系统")
	}
}

func TestLoadStandaloneAllowsPublicListenAddress(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("FUPAN_NO_AUTH", "true")
	t.Setenv("FUPAN_ADDR", ":8080")
	cfg, err := Load("")
	if err != nil || !cfg.NoAuth {
		t.Fatalf("独立部署应允许公开监听: cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadOIDCRequiresCompleteConfiguration(t *testing.T) {
	for _, missing := range []string{"FUPAN_OIDC_ISSUER", "FUPAN_OIDC_CLIENT_ID", "FUPAN_OIDC_CLIENT_SECRET", "FUPAN_REDIRECT_URL", "FUPAN_ADMIN_SUB"} {
		t.Run(missing, func(t *testing.T) {
			setValidOIDCEnv(t)
			t.Setenv(missing, "")
			if _, err := Load(""); err == nil {
				t.Fatalf("缺少 %s 时必须拒绝启动", missing)
			}
		})
	}
}

func TestLoadOIDCRequiresHTTPS(t *testing.T) {
	for _, key := range []string{"FUPAN_OIDC_ISSUER", "FUPAN_REDIRECT_URL"} {
		t.Run(key, func(t *testing.T) {
			setValidOIDCEnv(t)
			t.Setenv(key, "http://insecure.example.com/callback")
			if _, err := Load(""); err == nil {
				t.Fatalf("%s 使用 HTTP 时必须拒绝启动", key)
			}
		})
	}
}

func TestLoadOIDCParsesAllowedSubs(t *testing.T) {
	setValidOIDCEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("加载完整 OIDC 配置失败: %v", err)
	}
	if cfg.NoAuth {
		t.Fatal("OIDC 模式不应启用 NoAuth")
	}
	if cfg.OIDC.Issuer != "https://issuer.example.com" || cfg.OIDC.ClientID != "fupan-client" || cfg.OIDC.AdminSub != "admin-sub" {
		t.Fatalf("OIDC 配置加载错误: %+v", cfg.OIDC)
	}
	want := []string{"user-sub", "admin-sub", "other-sub"}
	if !reflect.DeepEqual(cfg.OIDC.AllowedSubs, want) {
		t.Fatalf("白名单解析错误: got %v want %v", cfg.OIDC.AllowedSubs, want)
	}
}

func TestLoadAllowedEndpointHostsIncludesPresetsAndCustomEnv(t *testing.T) {
	setValidOIDCEnv(t)
	t.Setenv("FUPAN_ALLOWED_ENDPOINT_HOSTS", " custom.example.com, proxy.example.com ")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("加载端点允许列表失败: %v", err)
	}
	want := []string{
		"api.deepseek.com", "open.bigmodel.cn", "api.openai.com",
		"api.tavily.com", "api.bochaai.com", "custom.example.com", "proxy.example.com",
	}
	for _, host := range want {
		found := false
		for _, got := range cfg.AllowedEndpointHosts {
			if got == host {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("端点允许列表缺少 %q: %v", host, cfg.AllowedEndpointHosts)
		}
	}
}

func TestLoadRejectsUnsupportedConfigFile(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("FUPAN_NO_AUTH", "true")
	t.Setenv("FUPAN_ADDR", "127.0.0.1:8080")
	if _, err := Load("custom.yml"); err == nil {
		t.Fatal("配置文件加载尚未实现时必须明确拒绝，不能静默使用其他配置")
	}
}

func TestLoadRejectsInvalidNoAuth(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("FUPAN_NO_AUTH", "sometimes")
	if _, err := Load(""); err == nil {
		t.Fatal("无效 NoAuth 布尔值必须报错")
	}
}
