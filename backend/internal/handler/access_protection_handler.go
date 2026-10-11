package handler

// Sakura extension: single writer, durable intent journal, scoped CN exception.
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

const accessQuotaUnit = 100.0

type AccessBalanceReader interface {
	TotalRecharged(context.Context, int64) (float64, error)
}
type AccessIPRecord struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id,omitempty"`
	IP        string    `json:"ip"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type accessResponse struct {
	CurrentIP      string           `json:"current_ip"`
	Quota          int              `json:"quota"`
	Used           int              `json:"used"`
	TotalRecharged float64          `json:"total_recharged"`
	Items          []AccessIPRecord `json:"items"`
}
type AccessProtectionHandler struct {
	dir                                      string
	reader                                   AccessBalanceReader
	zoneID, rulesetID, ruleID                string
	tokenFile, keyFile, apiURL, expectedBase string
	client                                   *http.Client
	mu                                       sync.Mutex
}

func NewAccessProtectionHandler(dataDir string, reader AccessBalanceReader) *AccessProtectionHandler {
	if dataDir == "" {
		dataDir = filepath.Join("data", "access-protection")
	}
	h := &AccessProtectionHandler{dir: dataDir, reader: reader, zoneID: os.Getenv("CLOUDFLARE_ZONE_ID"), rulesetID: os.Getenv("CLOUDFLARE_RULESET_ID"), ruleID: os.Getenv("CLOUDFLARE_RULE_ID"), tokenFile: os.Getenv("CLOUDFLARE_IP_TOKEN_FILE"), keyFile: os.Getenv("CLOUDFLARE_IP_TOKEN_KEY_FILE"), expectedBase: os.Getenv("CLOUDFLARE_BASE_EXPRESSION"), apiURL: "https://api.cloudflare.com/client/v4", client: &http.Client{Timeout: 15 * time.Second}}
	// Process-lifetime worker recovers pending transactions even without a client.
	go func() {
		for {
			h.mu.Lock()
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			_, _ = h.ready(ctx)
			cancel()
			h.mu.Unlock()
			time.Sleep(time.Minute)
		}
	}()
	return h
}

// Conservative public unicast validation; never accept a hostname/CIDR/expression.
var accessReserved = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range strings.Fields("0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.0.0.0/24 192.0.2.0/24 192.88.99.0/24 192.168.0.0/16 198.18.0.0/15 198.51.100.0/24 203.0.113.0/24 224.0.0.0/3 2001::/23 2001:db8::/32 2002::/16 3fff::/20") {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func validPublicIP(raw string) (string, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || ip.Zone() != "" || ip.Is4In6() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return "", errors.New("请输入单个有效公网 IPv4 或 IPv6 地址")
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return "", errors.New("请使用公网 IPv6 地址")
	}
	for _, p := range accessReserved {
		if p.Contains(ip) {
			return "", errors.New("请使用公网地址，而非内网或保留地址")
		}
	}
	return ip.String(), nil
}
func currentIP(c *gin.Context) string {
	// Only a configured immediate reverse proxy may provide X-Real-IP. That
	// proxy must overwrite the header after validating its own upstream chain.
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return ""
	}
	peer, err := netip.ParseAddr(host)
	if err == nil {
		for _, raw := range strings.Split(os.Getenv("ACCESS_TRUSTED_PROXY_CIDRS"), ",") {
			prefix, e := netip.ParsePrefix(strings.TrimSpace(raw))
			if e == nil && prefix.Contains(peer.Unmap()) {
				ip, _ := validPublicIP(c.GetHeader("X-Real-IP"))
				return ip
			}
		}
	}
	ip, _ := validPublicIP(host)
	return ip
}
func (h *AccessProtectionHandler) quota(ctx context.Context, id int64) (int, float64, error) {
	total, err := h.reader.TotalRecharged(ctx, id)
	if err != nil || math.IsNaN(total) || math.IsInf(total, 0) || total < 0 || total > 1e12 {
		return 0, 0, errors.New("累计计额读取失败")
	}
	return int(math.Floor(total / accessQuotaUnit)), total, nil
}
func ownRecords(s accessState, id int64) []AccessIPRecord {
	out := make([]AccessIPRecord, 0)
	for _, r := range s.Records {
		if r.UserID == id {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (h *AccessProtectionHandler) Get(c *gin.Context) {
	sub, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.ready(c.Request.Context())
	if err != nil {
		response.Error(c, 503, "同步状态待确认，请稍后刷新；持续失败请联系管理员")
		return
	}
	q, total, err := h.quota(c, sub.UserID)
	if err != nil {
		response.Error(c, 500, "累计计额读取失败")
		return
	}
	items := ownRecords(s, sub.UserID)
	response.Success(c, accessResponse{CurrentIP: currentIP(c), Quota: q, Used: len(items), TotalRecharged: total, Items: items})
}

type accessIPRequest struct {
	IP string `json:"ip" binding:"required"`
}

func (h *AccessProtectionHandler) Put(c *gin.Context)    { h.mutate(c, "") }
func (h *AccessProtectionHandler) Update(c *gin.Context) { h.mutate(c, c.Param("id")) }
func (h *AccessProtectionHandler) mutate(c *gin.Context, id string) {
	sub, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req accessIPRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "请输入 IP 地址")
		return
	}
	ip, err := validPublicIP(req.IP)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.ready(c.Request.Context())
	if err != nil {
		response.Error(c, 503, "同步状态待确认，请稍后刷新")
		return
	}
	idx := -1
	for i, r := range s.Records {
		if r.ID == id && r.UserID == sub.UserID {
			idx = i
			break
		}
	}
	if id != "" && idx < 0 {
		response.Error(c, 404, "IP 记录不存在")
		return
	}
	q, _, err := h.quota(c, sub.UserID)
	if err != nil {
		response.Error(c, 500, "累计计额读取失败")
		return
	}
	for _, r := range s.Records {
		if r.UserID == sub.UserID && r.IP == ip && r.ID != id {
			if id == "" {
				response.Success(c, gin.H{"id": r.ID, "ip": ip})
				return
			}
			response.Error(c, 409, "该 IP 已在你的记录中")
			return
		}
	}
	used := len(ownRecords(s, sub.UserID))
	if (idx < 0 && used >= q) || (idx >= 0 && used > q) {
		response.Error(c, 409, "可提交额度已用完")
		return
	}
	next := append([]AccessIPRecord{}, s.Records...)
	now := time.Now().UTC()
	if idx < 0 {
		if len(next) >= 200 {
			response.Error(c, 409, "站点 IP 容量已满，请联系管理员")
			return
		}
		var b [16]byte
		if _, err = rand.Read(b[:]); err != nil {
			response.Error(c, 500, "创建记录失败")
			return
		}
		id = hex.EncodeToString(b[:])
		next = append(next, AccessIPRecord{ID: id, UserID: sub.UserID, IP: ip, Status: "applied", CreatedAt: now, UpdatedAt: now})
	} else {
		next[idx].IP = ip
		next[idx].UpdatedAt = now
	}
	if err = h.commit(c.Request.Context(), &s, next); err != nil {
		response.Error(c, 503, "规则同步待确认，请刷新查看结果；持续失败请联系管理员")
		return
	}
	response.Success(c, gin.H{"id": id, "ip": ip})
}
func (h *AccessProtectionHandler) Delete(c *gin.Context) {
	sub, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.ready(c.Request.Context())
	if err != nil {
		response.Error(c, 503, "同步状态待确认，请稍后刷新")
		return
	}
	next := make([]AccessIPRecord, 0, len(s.Records))
	found := false
	for _, r := range s.Records {
		if r.ID == c.Param("id") && r.UserID == sub.UserID {
			found = true
		} else {
			next = append(next, r)
		}
	}
	if !found {
		response.Error(c, 404, "IP 记录不存在")
		return
	}
	if err = h.commit(c.Request.Context(), &s, next); err != nil {
		response.Error(c, 503, "规则同步待确认，请刷新查看结果；持续失败请联系管理员")
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *AccessProtectionHandler) RotateToken(c *gin.Context) {
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Error(c, http.StatusForbidden, "仅管理员可轮转令牌")
		return
	}
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if c.ShouldBindJSON(&req) != nil || len(req.Token) < 20 || len(req.Token) > 512 || strings.TrimSpace(req.Token) != req.Token {
		response.BadRequest(c, "令牌格式无效")
		return
	}
	for _, ch := range req.Token {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			response.BadRequest(c, "令牌格式无效")
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.ready(c.Request.Context())
	if err != nil {
		response.Error(c, 503, "同步状态待确认，令牌未更改")
		return
	}
	remote, err := h.ruleWithToken(c.Request.Context(), req.Token)
	if err != nil {
		response.BadRequest(c, "新令牌无法读取指定规则，令牌未更改")
		return
	}
	if s.LastRule != nil && !reflect.DeepEqual(remote, s.LastRule) || s.LastRule == nil && (remote["expression"] != h.expectedBase || remote["action"] != "block" || remote["enabled"] != true) {
		response.Error(c, 409, "规则与本地状态不一致，令牌未更改")
		return
	}
	if err = h.cfWithToken(c.Request.Context(), req.Token, http.MethodPatch, h.rulePath(), remote, nil); err != nil {
		response.BadRequest(c, "新令牌无法更新指定规则，令牌未更改")
		return
	}
	confirmed, err := h.ruleWithToken(c.Request.Context(), req.Token)
	if err != nil || !reflect.DeepEqual(confirmed, remote) {
		response.Error(c, 503, "规则回读未确认，令牌未更改")
		return
	}
	if err = h.storeToken(req.Token); err != nil {
		response.Error(c, 503, "令牌保存失败，旧令牌仍有效")
		return
	}
	response.Success(c, gin.H{"rotated": true})
}
