package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type GameIdentity struct {
	Name, UUID, Token, UserType, XUID string
}

var errNeedAccount = errors.New("conecte sua conta Minecraft para jogar (Configurações → Conta Minecraft)")

func gameIdentity(instID string) (GameIdentity, error) {
	a := store.Settings().MCAccount
	if a == nil {
		return GameIdentity{}, errNeedAccount
	}
	fresh, err := refreshMC(*a)
	if err != nil {
		var ne *NetError
		if errors.As(err, &ne) {
			appendLog(instID, "[Hypercube] Sem conexão para renovar a sessão ("+ne.Host+"). Abrindo em modo offline com a conta verificada "+a.Name+".")
			return GameIdentity{Name: a.Name, UUID: a.UUID, Token: "0", UserType: "legacy", XUID: "0"}, nil
		}
		return GameIdentity{}, fmt.Errorf("a sessão da conta Minecraft expirou, entre de novo em Configurações → Conta Minecraft (%v)", err)
	}
	x := fresh.XUID
	if x == "" {
		x = "0"
	}
	return GameIdentity{Name: fresh.Name, UUID: fresh.UUID, Token: fresh.AccessToken, UserType: "msa", XUID: x}, nil
}

type msToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func msForm(u string, form url.Values, v any) error {
	data, err := doRequest("POST", u, form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil && len(data) == 0 {
		return err
	}
	return json.Unmarshal(data, v)
}

var loginMu sync.Mutex
var loginCancel chan struct{}

type deviceCode struct {
	UserCode        string `json:"user_code"`
	DeviceCode      string `json:"device_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Error           string `json:"error"`
	ErrorDesc       string `json:"error_description"`
}

func mcLoginStart() (*deviceCode, error) {
	cid := strings.TrimSpace(store.Settings().MSClientID)
	if cid == "" {
		return nil, fmt.Errorf("configure o Client ID do seu app do Azure primeiro")
	}
	var dc deviceCode
	if err := msForm("https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode",
		url.Values{"client_id": {cid}, "scope": {"XboxLive.signin offline_access"}}, &dc); err != nil {
		return nil, err
	}
	if dc.DeviceCode == "" {
		if strings.Contains(dc.ErrorDesc, "AADSTS700016") || dc.Error == "unauthorized_client" {
			return nil, fmt.Errorf("Client ID não encontrado. Confira se copiou o “Application (client) ID” e se o app aceita contas pessoais Microsoft")
		}
		if dc.ErrorDesc != "" {
			return nil, fmt.Errorf("%s", firstLine(dc.ErrorDesc))
		}
		return nil, fmt.Errorf("não foi possível iniciar o login Microsoft")
	}
	loginMu.Lock()
	if loginCancel != nil {
		close(loginCancel)
	}
	loginCancel = make(chan struct{})
	cancel := loginCancel
	loginMu.Unlock()
	go mcLoginPoll(cid, dc, cancel)
	return &dc, nil
}

func mcLoginCancel() {
	loginMu.Lock()
	if loginCancel != nil {
		close(loginCancel)
		loginCancel = nil
	}
	loginMu.Unlock()
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func mcLoginPoll(cid string, dc deviceCode, cancel chan struct{}) {
	interval := time.Duration(max(dc.Interval, 3)) * time.Second
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-cancel:
			return
		case <-time.After(interval):
		}
		var tok msToken
		if err := msForm("https://login.microsoftonline.com/consumers/oauth2/v2.0/token", url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {cid},
			"device_code": {dc.DeviceCode},
		}, &tok); err != nil {
			continue
		}
		switch tok.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "":
		case "authorization_declined":
			hub.Emit("mcaccount-error", "login cancelado na página da Microsoft")
			return
		case "invalid_client", "unauthorized_client":
			hub.Emit("mcaccount-error", "o app do Azure não permite este login: ative “Allow public client flows” em Authentication")
			return
		default:
			hub.Emit("mcaccount-error", firstLine(tok.ErrorDesc))
			return
		}
		acc, err := minecraftFromMS(tok)
		if err != nil {
			hub.Emit("mcaccount-error", err.Error())
			return
		}
		store.UpdateSettings(func(st *Settings) { st.MCAccount = &acc })
		hub.Emit("mcaccount", publicSettings()["mcAccount"])
		return
	}
	hub.Emit("mcaccount-error", "o código expirou, tente de novo")
}

func minecraftFromMS(tok msToken) (MCAccount, error) {
	var xbl struct {
		Token         string `json:"Token"`
		DisplayClaims struct {
			Xui []struct {
				Uhs string `json:"uhs"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	if err := postJSON("https://user.auth.xboxlive.com/user/authenticate", map[string]any{
		"Properties":   map[string]any{"AuthMethod": "RPS", "SiteName": "user.auth.xboxlive.com", "RpsTicket": "d=" + tok.AccessToken},
		"RelyingParty": "http://auth.xboxlive.com", "TokenType": "JWT",
	}, &xbl, map[string]string{"x-xbl-contract-version": "1"}); err != nil || len(xbl.DisplayClaims.Xui) == 0 {
		return MCAccount{}, fmt.Errorf("falha no Xbox Live: %w", err)
	}
	uhs := xbl.DisplayClaims.Xui[0].Uhs

	var xsts struct {
		Token         string `json:"Token"`
		XErr          int64  `json:"XErr"`
		DisplayClaims struct {
			Xui []struct {
				Xid string `json:"xid"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	body, _ := json.Marshal(map[string]any{
		"Properties":   map[string]any{"SandboxId": "RETAIL", "UserTokens": []string{xbl.Token}},
		"RelyingParty": "rp://api.minecraftservices.com/", "TokenType": "JWT",
	})
	data, err := doRequest("POST", "https://xsts.auth.xboxlive.com/xsts/authorize", body, map[string]string{"x-xbl-contract-version": "1"})
	json.Unmarshal(data, &xsts)
	var ne *NetError
	if errors.As(err, &ne) {
		return MCAccount{}, err
	}
	if err != nil || xsts.Token == "" {
		switch xsts.XErr {
		case 2148916233:
			return MCAccount{}, fmt.Errorf("esta conta Microsoft ainda não tem perfil Xbox. Entre uma vez em minecraft.net para criar")
		case 2148916235:
			return MCAccount{}, fmt.Errorf("o Xbox Live não está disponível no seu país")
		case 2148916236, 2148916237:
			return MCAccount{}, fmt.Errorf("a conta precisa de verificação de idade no site do Xbox")
		case 2148916238:
			return MCAccount{}, fmt.Errorf("conta de menor de idade: um adulto precisa adicioná-la a uma Família Microsoft")
		}
		return MCAccount{}, fmt.Errorf("falha na autorização Xbox (XSTS)")
	}
	xid := ""
	if len(xsts.DisplayClaims.Xui) > 0 {
		xid = xsts.DisplayClaims.Xui[0].Xid
	}

	var mc struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := postJSON("https://api.minecraftservices.com/authentication/login_with_xbox", map[string]any{
		"identityToken": "XBL3.0 x=" + uhs + ";" + xsts.Token,
	}, &mc); err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Status == http.StatusForbidden {
			return MCAccount{}, fmt.Errorf("a Mojang ainda não aprovou seu app do Azure. Envie o formulário de aprovação (veja o guia) e tente de novo depois que for aceito")
		}
		return MCAccount{}, fmt.Errorf("falha ao entrar no Minecraft: %w", err)
	}

	var ent struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := getJSON("https://api.minecraftservices.com/entitlements/mcstore", &ent, map[string]string{"Authorization": "Bearer " + mc.AccessToken}); err != nil {
		return MCAccount{}, fmt.Errorf("não foi possível verificar a licença do jogo: %w", err)
	}
	owns := false
	for _, it := range ent.Items {
		n := strings.ToLower(it.Name)
		if strings.Contains(n, "minecraft") || strings.Contains(n, "game_pass") {
			owns = true
		}
	}
	if !owns {
		return MCAccount{}, fmt.Errorf("esta conta Microsoft não possui o Minecraft: Java Edition")
	}
	var prof struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := getJSON("https://api.minecraftservices.com/minecraft/profile", &prof, map[string]string{"Authorization": "Bearer " + mc.AccessToken}); err != nil || prof.ID == "" {
		if errors.As(err, &ne) {
			return MCAccount{}, err
		}
		return MCAccount{}, fmt.Errorf("esta conta Microsoft não tem o Minecraft: Java Edition (ou ainda não escolheu um nick em minecraft.net)")
	}
	return MCAccount{
		Name: prof.Name, UUID: prof.ID, XUID: xid,
		AccessToken: mc.AccessToken, RefreshToken: tok.RefreshToken,
		ExpiresAt: time.Now().Unix() + int64(mc.ExpiresIn) - 300,
	}, nil
}

func refreshMC(a MCAccount) (MCAccount, error) {
	if time.Now().Unix() < a.ExpiresAt {
		return a, nil
	}
	cid := store.Settings().MSClientID
	var tok msToken
	if err := msForm("https://login.microsoftonline.com/consumers/oauth2/v2.0/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {a.RefreshToken},
		"scope": {"XboxLive.signin offline_access"},
	}, &tok); err != nil {
		return a, err
	}
	if tok.Error != "" {
		return a, fmt.Errorf("%s", firstLine(tok.ErrorDesc))
	}
	n, err := minecraftFromMS(tok)
	if err != nil {
		return a, err
	}
	store.UpdateSettings(func(st *Settings) { st.MCAccount = &n })
	hub.Emit("mcaccount", publicSettings()["mcAccount"])
	return n, nil
}

func registerMCAuthAPI(handle func(string, http.HandlerFunc)) {
	handle("POST /api/mc/login", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ClientID string `json:"clientId"`
		}
		decode(r, &b)
		if cid := strings.TrimSpace(b.ClientID); cid != "" {
			store.UpdateSettings(func(st *Settings) { st.MSClientID = cid })
		}
		dc, err := mcLoginStart()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]any{"code": dc.UserCode, "url": dc.VerificationURI, "expires": dc.ExpiresIn})
	})
	handle("POST /api/mc/cancel", func(w http.ResponseWriter, r *http.Request) {
		mcLoginCancel()
		writeJSONResp(w, map[string]bool{"ok": true})
	})
	handle("DELETE /api/mc/account", func(w http.ResponseWriter, r *http.Request) {
		store.UpdateSettings(func(st *Settings) { st.MCAccount = nil })
		ps := publicSettings()
		hub.Emit("mcaccount", ps["mcAccount"])
		writeJSONResp(w, ps)
	})
}
