package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/accounts"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) mcpOrigin(origin string) bool {
	return origin == s.config.PublicURL || origin == "https://chatgpt.com" || origin == "https://claude.ai" || origin == "https://claude.com"
}
func (s *Server) remoteMCP() http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		p, _, e := s.oauth.Authenticate(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if e != nil {
			return nil
		}
		return s.mcpServer(r, p)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// OAuth access tokens alone authenticate this resource. Cookies, manual REST
		// grants, upstream Google tokens, authorization codes and refresh tokens do not.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			s.mcpUnauthorized(w)
			return
		}
		if _, _, e := s.oauth.Authenticate(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); e != nil {
			if errors.Is(e, accounts.ErrUnavailable) {
				fail(w, 503, "Account storage unavailable.")
			} else {
				s.mcpUnauthorized(w)
			}
			return
		}
		if !s.quota(w, r, "mcp-request", "all", 600, time.Minute) {
			return
		}
		handler.ServeHTTP(w, r)
	})
}
func (s *Server) mcpUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+s.config.PublicURL+`/.well-known/oauth-protected-resource/mcp", scope="read draft offline_access"`)
	oauthError(w, 401, "invalid_token")
}

type mcpArgs struct {
	Prompt   string           `json:"prompt,omitempty"`
	Items    *[]sdk.Selection `json:"items,omitempty"`
	Page     int              `json:"page,omitempty"`
	PageSize int              `json:"page_size,omitempty"`
	OrderID  string           `json:"order_id,omitempty"`
}

func decodeArgs(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing input")
	}
	return nil
}
func mcpResult(value any, err error) *mcp.CallToolResult {
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
	}
	raw, _ := json.Marshal(value)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: value}
}
func objectSchema(props map[string]any, required ...string) map[string]any {
	v := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		v["required"] = required
	}
	return v
}
func (s *Server) mcpServer(r *http.Request, p accounts.Principal) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "aria-commerce", Version: sdk.Version}, nil)
	no := false
	add := func(name, description string, input any, read bool, run func(context.Context, mcpArgs) (any, error)) {
		scope := "read"
		if name == "paypal_list_invoices" {
			scope = "paypal.read"
		}
		if !read {
			scope = "draft"
		}
		t := &mcp.Tool{Name: name, Description: description, InputSchema: input, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: read, IdempotentHint: read, DestructiveHint: &no, OpenWorldHint: &no}, Meta: mcp.Meta{"securitySchemes": []any{map[string]any{"type": "oauth2", "scopes": []string{scope}}}}}
		if name == "paypal_list_invoices" {
			yes := true
			t.Annotations.OpenWorldHint = &yes
		}
		if name == "commerce_profile" {
			t.Meta["openai/profile"] = true
			t.OutputSchema = objectSchema(map[string]any{"id": map[string]any{"type": "string", "minLength": 1}, "nickname": map[string]any{"type": "string"}}, "id")
		}
		server.AddTool(t, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if len(request.Params.Arguments) == 0 {
				request.Params.Arguments = json.RawMessage(`{}`)
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(request.Params.Arguments, &fields) != nil || fields == nil {
				return mcpResult(nil, errors.New("Arguments must be an object.")), nil
			}
			allowed := map[string]bool{}
			if name == "paypal_list_invoices" {
				allowed["page"] = true
				allowed["page_size"] = true
			}
			if name == "commerce_get_order" {
				allowed["order_id"] = true
			}
			if name == "commerce_draft_order" {
				allowed["prompt"] = true
				allowed["items"] = true
				if len(fields) != 1 {
					return mcpResult(nil, errors.New("Provide exactly one of items or prompt.")), nil
				}
			}
			for key := range fields {
				if !allowed[key] {
					return mcpResult(nil, errors.New("Unexpected tool argument.")), nil
				}
			}
			var a mcpArgs
			if len(request.Params.Arguments) == 0 {
				request.Params.Arguments = json.RawMessage(`{}`)
			}
			if e := decodeArgs(request.Params.Arguments, &a); e != nil {
				return mcpResult(nil, errors.New("Invalid tool arguments.")), nil
			}
			// Recheck grant and membership at execution time, including revocation that
			// happened after tools/list or initialize on another replica.
			current, _, e := s.oauth.Authenticate(ctx, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if e != nil || current.UserID != p.UserID || !current.Allows(scope) {
				return mcpResult(nil, errors.New("Connection expired, revoked, or lacks permission. Reconnect in your client.")), nil
			}
			value, e := run(ctx, a)
			return mcpResult(value, e), nil
		})
	}
	empty := objectSchema(map[string]any{})
	checkEmpty := func(a mcpArgs) bool {
		return a.Prompt == "" && a.Items == nil && a.OrderID == "" && a.Page == 0 && a.PageSize == 0
	}
	add("commerce_profile", "Return the authenticated profile. Its opaque ID remains stable across reconnections and refresh.", empty, true, func(ctx context.Context, a mcpArgs) (any, error) {
		if !checkEmpty(a) {
			return nil, errors.New("This tool accepts no arguments.")
		}
		return map[string]string{"id": p.UserID, "nickname": "Aria Commerce · " + p.MerchantID}, nil
	})
	add("paypal_connection_status", "Check whether this user has separately authorized a PayPal merchant. An Aria login or checkout provider is not PayPal account authorization. No credentials are returned.", empty, true, func(ctx context.Context, a mcpArgs) (any, error) {
		if !checkEmpty(a) {
			return nil, errors.New("This tool accepts no arguments.")
		}
		return s.config.PayPalAccounts.Status(ctx, p.UserID)
	})
	add("paypal_list_invoices", "Read actual PayPal invoices for this user's independently authorized merchant. Requires explicit paypal.read client consent and provider invoices.read capability. Never returns Aria catalogue or test orders as PayPal data. No write or payment actions.", objectSchema(map[string]any{"page": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000}, "page_size": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}), true, func(ctx context.Context, a mcpArgs) (any, error) {
		page, size := a.Page, a.PageSize
		if page == 0 {
			page = 1
		}
		if size == 0 {
			size = 20
		}
		result, e := s.config.PayPalAccounts.Invoices(ctx, p.UserID, page, size)
		if e != nil {
			return nil, paypalReadError(e)
		}
		return result, nil
	})
	if !s.config.LegacyTools {
		return server
	}
	add("commerce_catalog", "Read trusted catalogue SKUs and integer-cent prices. Use these prices and identifiers for draft proposals.", empty, true, func(ctx context.Context, a mcpArgs) (any, error) {
		if !checkEmpty(a) {
			return nil, errors.New("This tool accepts no arguments.")
		}
		items, e := s.catalogue().Products(ctx)
		return map[string]any{"products": items}, e
	})
	add("commerce_get_order", "Read an order in the connected merchant account. Requires an exact order ID.", objectSchema(map[string]any{"order_id": map[string]any{"type": "string", "pattern": "^[a-f0-9]{32}$"}}, "order_id"), true, func(ctx context.Context, a mcpArgs) (any, error) {
		if !reviewOrderID.MatchString(a.OrderID) || a.Items != nil || a.Prompt != "" {
			return nil, errors.New("Provide only a valid order_id.")
		}
		o, e := s.service.Get(ctx, actor(identity{owner: s.merchantOwner(), bearer: true}), a.OrderID)
		if e != nil {
			return nil, e
		}
		return struct {
			sdk.Order
			ReviewURL string `json:"review_url"`
		}{public(o), s.config.PublicURL + "/?order=" + o.ID}, nil
	})
	if p.Allows("draft") {
		selection := objectSchema(map[string]any{"sku": map[string]any{"type": "string"}, "quantity": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, "sku", "quantity")
		input := objectSchema(map[string]any{"items": map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": selection}, "prompt": map[string]any{"type": "string", "minLength": 3, "maxLength": 2000}})
		input["oneOf"] = []any{map[string]any{"required": []string{"items"}, "not": map[string]any{"required": []string{"prompt"}}}, map[string]any{"required": []string{"prompt"}, "not": map[string]any{"required": []string{"items"}}}}
		add("commerce_draft_order", "Create an UNAPPROVED order. Read commerce_catalog, interpret the user's request using your own model, then submit only sku and quantity in items. The server resolves prices. Alternatively submit prompt to use a configured server model. Provide exactly one. Never guess ambiguous selections. Returns a review URL. Cannot approve, pay, capture or refund. A retry may create another draft; do not retry after an ambiguous timeout.", input, false, func(ctx context.Context, a mcpArgs) (any, error) {
			if a.OrderID != "" {
				return nil, errors.New("order_id is not accepted.")
			}
			body, _ := json.Marshal(a)
			req := r.Clone(ctx)
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.Method = "POST"
			req.URL.Path = "/api/orders"
			out := &toolResponse{header: http.Header{}, status: 200}
			s.draftAs(out, req, identity{owner: s.merchantOwner(), bearer: true, principal: p})
			if out.status >= 400 {
				return nil, errors.New(out.body.String())
			}
			var v map[string]any
			if json.Unmarshal(out.body.Bytes(), &v) != nil {
				return nil, errors.New("Invalid order response.")
			}
			orderID, validID := v["id"].(string)
			if !validID || !reviewOrderID.MatchString(orderID) {
				return nil, errors.New("Invalid order response.")
			}
			v["review_url"] = s.config.PublicURL + "/?order=" + orderID
			return v, nil
		})
	}
	return server
}

// Captures the shared draft handler's result, preserving its validation, quotas,
// audit logging and human-only workflow restrictions without an HTTP loopback.
type toolResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *toolResponse) Header() http.Header         { return w.header }
func (w *toolResponse) WriteHeader(n int)           { w.status = n }
func (w *toolResponse) Write(v []byte) (int, error) { return w.body.Write(v) }
