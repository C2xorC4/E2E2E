package admin

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"e2e-chat/internal/db"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler contains admin/moderation handlers
type Handler struct {
	db *db.DB
}

// NewHandler creates a new admin handler
func NewHandler(database *db.DB) *Handler {
	return &Handler{db: database}
}

// SetupRoutes configures admin routes
func (h *Handler) SetupRoutes(r *gin.Engine) {
	// Serve static dashboard at root
	r.GET("/", h.Dashboard)
	r.GET("/admin", h.Dashboard)
	r.GET("/admin/users", h.Users)
	r.GET("/admin/users/:id", h.UserDetail)
	r.GET("/admin/chats", h.Chats)
	r.GET("/admin/chats/:id", h.ChatDetail)
	r.GET("/admin/messages", h.Messages)
	r.GET("/admin/search", h.Search)

	// API endpoints for moderation actions
	r.DELETE("/admin/api/messages/:id", h.DeleteMessage)
	r.DELETE("/admin/api/users/:id", h.DeleteUser)
}

// Dashboard renders the main moderation dashboard
func (h *Handler) Dashboard(c *gin.Context) {
	ctx := c.Request.Context()

	// Get stats
	users, _ := h.db.GetAllUsers(ctx)
	chats, _ := h.db.GetAllChats(ctx)

	// Count recent messages (last 24h)
	recentMessages, _ := h.db.GetRecentMessages(ctx, 24*time.Hour, 100)

	data := map[string]interface{}{
		"Title":          "Moderation Dashboard",
		"UserCount":      len(users),
		"ChatCount":      len(chats),
		"RecentMsgCount": len(recentMessages),
		"RecentMessages": recentMessages,
	}

	renderTemplate(c, "dashboard", data)
}

// Users lists all users with sorting
func (h *Handler) Users(c *gin.Context) {
	ctx := c.Request.Context()

	// Get sorting parameters
	sortBy := c.DefaultQuery("sort", "created_at")
	order := c.DefaultQuery("order", "desc")

	users, err := h.db.GetAllUsersSorted(ctx, sortBy, order)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error loading users: %v", err)
		return
	}

	// Toggle order for next click
	nextOrder := "asc"
	if order == "asc" {
		nextOrder = "desc"
	}

	data := map[string]interface{}{
		"Title":     "All Users",
		"Users":     users,
		"SortBy":    sortBy,
		"Order":     order,
		"NextOrder": nextOrder,
	}

	renderTemplate(c, "users", data)
}

// UserDetail shows a specific user's details and activity
func (h *Handler) UserDetail(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := h.db.GetUserByID(ctx, userID)
	if err != nil {
		c.String(http.StatusNotFound, "User not found")
		return
	}

	// Get user's chats (all including left/self-destructed)
	chats, _ := h.db.GetAllChatsForUser(ctx, userID)

	// Get user's recent messages
	messages, _ := h.db.GetMessagesByUserID(ctx, userID, 50)

	// Get user's devices
	devices, _ := h.db.GetDevicesByUserID(ctx, userID)

	// Get unique communication partners count
	uniquePartners, _ := h.db.GetUserUniqueCommunicationCount(ctx, userID)

	// Get total sent messages count
	sentMessageCount, _ := h.db.GetUserMessageCount(ctx, userID)

	// Get communication partners details
	partners, _ := h.db.GetUserCommunicationPartners(ctx, userID)

	// Get all conversations involving this user (for "see who else talks to them")
	conversationsInvolvingUser, _ := h.db.GetChatsInvolvingUser(ctx, userID)

	data := map[string]interface{}{
		"Title":                  fmt.Sprintf("User: %s", user.Username),
		"User":                   user,
		"Chats":                  chats,
		"Messages":               messages,
		"Devices":                devices,
		"UniquePartners":         uniquePartners,
		"SentMessageCount":       sentMessageCount,
		"CommunicationPartners":  partners,
		"ConversationsInvolving": conversationsInvolvingUser,
	}

	renderTemplate(c, "user_detail", data)
}

// Chats lists all chats with filtering and sorting
func (h *Handler) Chats(c *gin.Context) {
	ctx := c.Request.Context()

	// Get filter params
	statusFilter := c.Query("status")
	participantFilter := c.Query("participant")
	messageQuery := c.Query("q")

	// Get sorting parameters
	sortBy := c.DefaultQuery("sort", "created_at")
	order := c.DefaultQuery("order", "desc")

	// Enrich with participant info
	type ChatInfo struct {
		ID                 uuid.UUID
		Name               string
		IsGroup            bool
		Status             string
		CreatedAt          time.Time
		UpdatedAt          time.Time
		Participants       []string
		FormerParticipants []string
		MessageCount       int
		ParticipantCount   int
		LastMessageAt      *time.Time
	}

	var chatInfos []ChatInfo

	// If advanced search is requested
	if statusFilter != "" || participantFilter != "" || messageQuery != "" {
		params := db.ChatSearchParams{
			Status:          statusFilter,
			ParticipantName: participantFilter,
			MessageQuery:    messageQuery,
		}
		results, err := h.db.SearchChatsAdvanced(ctx, params)
		if err != nil {
			c.String(http.StatusInternalServerError, "Error searching chats: %v", err)
			return
		}

		for _, r := range results {
			name := ""
			if r.ChatName != nil {
				name = *r.ChatName
			}
			if name == "" && r.ParticipantNames != "" {
				name = "DM: " + r.ParticipantNames
			}

			var participants, formerParticipants []string
			if r.ParticipantNames != "" {
				participants = splitAndTrim(r.ParticipantNames)
			}
			if r.FormerParticipants != "" {
				formerParticipants = splitAndTrim(r.FormerParticipants)
			}

			chatInfos = append(chatInfos, ChatInfo{
				ID:                 r.ChatID,
				Name:               name,
				IsGroup:            r.IsGroup,
				Status:             r.Status,
				CreatedAt:          r.CreatedAt,
				Participants:       participants,
				FormerParticipants: formerParticipants,
				MessageCount:       r.MessageCount,
				ParticipantCount:   r.ParticipantCount,
				LastMessageAt:      r.LastMessageAt,
			})
		}
	} else {
		// Default: get all chats with status and stats, sorted
		chats, err := h.db.GetAllChatsWithStatsSorted(ctx, sortBy, order)
		if err != nil {
			c.String(http.StatusInternalServerError, "Error loading chats: %v", err)
			return
		}

		for _, chat := range chats {
			participants, _ := h.db.GetAllChatParticipants(ctx, chat.ID)

			var pNames, formerNames []string
			for _, p := range participants {
				if p.Status == "active" || p.Status == "accepted" {
					pNames = append(pNames, p.Username)
				} else {
					suffix := "left"
					if p.LeaveReason != nil {
						suffix = *p.LeaveReason
					}
					formerNames = append(formerNames, fmt.Sprintf("%s (%s)", p.Username, suffix))
				}
			}

			name := ""
			if chat.Name != nil {
				name = *chat.Name
			}
			if name == "" && len(pNames) > 0 {
				name = fmt.Sprintf("DM: %v", pNames)
			}

			chatInfos = append(chatInfos, ChatInfo{
				ID:                 chat.ID,
				Name:               name,
				IsGroup:            chat.IsGroup,
				Status:             chat.Status,
				CreatedAt:          chat.CreatedAt,
				UpdatedAt:          chat.UpdatedAt,
				Participants:       pNames,
				FormerParticipants: formerNames,
				MessageCount:       chat.MessageCount,
				ParticipantCount:   chat.ParticipantCount,
				LastMessageAt:      chat.LastMessageAt,
			})
		}
	}

	// Toggle order for next click
	nextOrder := "asc"
	if order == "asc" {
		nextOrder = "desc"
	}

	data := map[string]interface{}{
		"Title":             "All Chats",
		"Chats":             chatInfos,
		"StatusFilter":      statusFilter,
		"ParticipantFilter": participantFilter,
		"MessageQuery":      messageQuery,
		"SortBy":            sortBy,
		"Order":             order,
		"NextOrder":         nextOrder,
	}

	renderTemplate(c, "chats", data)
}

// splitAndTrim splits a comma-separated string and trims whitespace
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ", ")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// ChatDetail shows all messages in a chat
func (h *Handler) ChatDetail(c *gin.Context) {
	ctx := c.Request.Context()

	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid chat ID")
		return
	}

	chat, err := h.db.GetChatByID(ctx, chatID)
	if err != nil {
		c.String(http.StatusNotFound, "Chat not found")
		return
	}

	// Get pagination params
	limit := 100
	offset := 0
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	messages, _ := h.db.GetMessagesByChat(ctx, chatID, limit, offset)
	allParticipants, _ := h.db.GetAllChatParticipants(ctx, chatID)
	totalCount, _ := h.db.GetChatMessageCount(ctx, chatID)

	// Build username map and separate active/former participants
	userMap := make(map[string]string)
	var activeParticipants, formerParticipants []*db.ParticipantWithStatus
	for _, p := range allParticipants {
		userMap[p.ID.String()] = p.Username
		if p.Status == "active" || p.Status == "accepted" {
			activeParticipants = append(activeParticipants, p)
		} else {
			formerParticipants = append(formerParticipants, p)
		}
	}

	chatName := ""
	if chat.Name != nil {
		chatName = *chat.Name
	}
	if chatName == "" {
		chatName = chat.ID.String()[:8]
	}

	// Determine chat status (would need to query, using simple approach here)
	chatStatus := "active"
	if len(activeParticipants) == 0 {
		chatStatus = "orphaned"
	}
	for _, p := range formerParticipants {
		if p.Status == "self_destructed" {
			chatStatus = "self_destructed"
			break
		}
	}

	data := map[string]interface{}{
		"Title":              fmt.Sprintf("Chat: %s", chatName),
		"Chat":               chat,
		"ChatStatus":         chatStatus,
		"Messages":           messages,
		"ActiveParticipants": activeParticipants,
		"FormerParticipants": formerParticipants,
		"UserMap":            userMap,
		"TotalCount":         totalCount,
		"Limit":              limit,
		"Offset":             offset,
		"NextOffset":         offset + limit,
		"PrevOffset":         max(0, offset-limit),
		"HasMore":            offset+limit < totalCount,
		"HasPrev":            offset > 0,
	}

	renderTemplate(c, "chat_detail", data)
}

// Messages lists all messages with filtering
func (h *Handler) Messages(c *gin.Context) {
	ctx := c.Request.Context()

	limit := 100
	offset := 0
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	messages, _ := h.db.GetAllMessages(ctx, limit, offset)

	// Build user map for display
	users, _ := h.db.GetAllUsers(ctx)
	userMap := make(map[string]string)
	for _, u := range users {
		userMap[u.ID.String()] = u.Username
	}

	data := map[string]interface{}{
		"Title":      "All Messages",
		"Messages":   messages,
		"UserMap":    userMap,
		"Limit":      limit,
		"Offset":     offset,
		"NextOffset": offset + limit,
		"PrevOffset": max(0, offset-limit),
		"HasMore":    len(messages) == limit,
		"HasPrev":    offset > 0,
	}

	renderTemplate(c, "messages", data)
}

// Search performs full-text search across all messages
func (h *Handler) Search(c *gin.Context) {
	ctx := c.Request.Context()
	query := c.Query("q")

	var messages []*db.MessageWithUser
	if query != "" {
		messages, _ = h.db.SearchAllMessages(ctx, query, 100)
	}

	// Build user map
	users, _ := h.db.GetAllUsers(ctx)
	userMap := make(map[string]string)
	for _, u := range users {
		userMap[u.ID.String()] = u.Username
	}

	data := map[string]interface{}{
		"Title":    "Search Messages",
		"Query":    query,
		"Messages": messages,
		"UserMap":  userMap,
	}

	renderTemplate(c, "search", data)
}

// DeleteMessage soft-deletes a message
func (h *Handler) DeleteMessage(c *gin.Context) {
	ctx := c.Request.Context()

	msgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid message ID"})
		return
	}

	if err := h.db.DeleteMessage(ctx, msgID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// DeleteUser deletes a user account
func (h *Handler) DeleteUser(c *gin.Context) {
	ctx := c.Request.Context()

	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	if err := h.db.DeleteUser(ctx, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// HTML Templates
func renderTemplate(c *gin.Context, name string, data map[string]interface{}) {
	tmpl := template.Must(template.New(name).Funcs(template.FuncMap{
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
		"formatTime": func(t time.Time) string {
			return t.Format("2006-01-02 15:04:05")
		},
		"shortID": func(id uuid.UUID) string {
			return id.String()[:8]
		},
		"add": func(a, b int) int {
			return a + b
		},
		"min": func(a, b int) int {
			if a < b {
				return a
			}
			return b
		},
	}).Parse(baseTemplate + templates[name]))

	c.Header("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(c.Writer, data)
}

const baseTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}} - E2E Chat Admin</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #1a1a2e;
            color: #eee;
            line-height: 1.6;
        }
        .container { max-width: 1400px; margin: 0 auto; padding: 20px; }
        nav {
            background: #16213e;
            padding: 15px 20px;
            margin-bottom: 20px;
            border-radius: 8px;
            display: flex;
            gap: 20px;
            align-items: center;
        }
        nav a {
            color: #4ecca3;
            text-decoration: none;
            padding: 8px 16px;
            border-radius: 4px;
            transition: background 0.2s;
        }
        nav a:hover { background: #0f3460; }
        nav .brand {
            font-weight: bold;
            font-size: 1.2em;
            color: #fff;
            margin-right: auto;
        }
        h1 { color: #4ecca3; margin-bottom: 20px; }
        h2 { color: #4ecca3; margin: 20px 0 10px; font-size: 1.2em; }
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        .stat-card {
            background: #16213e;
            padding: 20px;
            border-radius: 8px;
            text-align: center;
        }
        .stat-card .number {
            font-size: 2.5em;
            color: #4ecca3;
            font-weight: bold;
        }
        .stat-card .label { color: #888; }
        table {
            width: 100%;
            border-collapse: collapse;
            background: #16213e;
            border-radius: 8px;
            overflow: hidden;
        }
        th, td {
            padding: 12px 15px;
            text-align: left;
            border-bottom: 1px solid #0f3460;
        }
        th {
            background: #0f3460;
            color: #4ecca3;
            font-weight: 600;
        }
        th a {
            color: #4ecca3;
            text-decoration: none;
            display: block;
            white-space: nowrap;
        }
        th a:hover {
            color: #fff;
            text-decoration: underline;
        }
        tr:hover { background: #1a2744; }
        a { color: #4ecca3; }
        .message-content {
            background: #0f3460;
            padding: 10px 15px;
            border-radius: 8px;
            margin: 5px 0;
            max-width: 600px;
        }
        .meta { color: #888; font-size: 0.85em; }
        .btn {
            display: inline-block;
            padding: 8px 16px;
            background: #4ecca3;
            color: #1a1a2e;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            text-decoration: none;
            font-size: 0.9em;
        }
        .btn:hover { background: #3db892; }
        .btn-danger { background: #e74c3c; color: #fff; }
        .btn-danger:hover { background: #c0392b; }
        .search-box {
            display: flex;
            gap: 10px;
            margin-bottom: 20px;
        }
        .search-box input {
            flex: 1;
            padding: 12px 15px;
            border: none;
            border-radius: 4px;
            background: #16213e;
            color: #fff;
            font-size: 1em;
        }
        .search-box input:focus { outline: 2px solid #4ecca3; }
        .pagination {
            display: flex;
            gap: 10px;
            margin-top: 20px;
            justify-content: center;
        }
        .badge {
            display: inline-block;
            padding: 2px 8px;
            border-radius: 12px;
            font-size: 0.8em;
            background: #4ecca3;
            color: #1a1a2e;
        }
        .badge-group { background: #9b59b6; color: #fff; }
        .badge-orphaned { background: #f39c12; color: #1a1a2e; }
        .badge-destructed { background: #e74c3c; color: #fff; }
        .empty {
            text-align: center;
            padding: 40px;
            color: #888;
        }
    </style>
</head>
<body>
    <div class="container">
        <nav>
            <span class="brand">E2E Chat Admin</span>
            <a href="/admin">Dashboard</a>
            <a href="/admin/users">Users</a>
            <a href="/admin/chats">Chats</a>
            <a href="/admin/messages">Messages</a>
            <a href="/admin/search">Search</a>
        </nav>
        {{template "content" .}}
    </div>
    <script>
        async function deleteMessage(id) {
            if (!confirm('Delete this message?')) return;
            const resp = await fetch('/admin/api/messages/' + id, {method: 'DELETE'});
            if (resp.ok) location.reload();
            else alert('Failed to delete');
        }
        async function deleteUser(id) {
            if (!confirm('Delete this user and all their data?')) return;
            const resp = await fetch('/admin/api/users/' + id, {method: 'DELETE'});
            if (resp.ok) location.href = '/admin/users';
            else alert('Failed to delete');
        }
    </script>
</body>
</html>
`

var templates = map[string]string{
	"dashboard": `
{{define "content"}}
<h1>Moderation Dashboard</h1>
<div class="stats">
    <div class="stat-card">
        <div class="number">{{.UserCount}}</div>
        <div class="label">Total Users</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.ChatCount}}</div>
        <div class="label">Total Chats</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.RecentMsgCount}}</div>
        <div class="label">Messages (24h)</div>
    </div>
</div>

<h2>Recent Messages</h2>
{{if .RecentMessages}}
<table>
    <thead>
        <tr>
            <th>Time</th>
            <th>Sender</th>
            <th>Content</th>
            <th>Chat</th>
        </tr>
    </thead>
    <tbody>
        {{range .RecentMessages}}
        <tr>
            <td class="meta">{{formatTime .CreatedAt}}</td>
            <td><a href="/admin/users/{{.SenderID}}">{{.SenderUsername}}</a></td>
            <td>{{truncate .Content 80}}</td>
            <td><a href="/admin/chats/{{.ChatID}}">{{shortID .ChatID}}</a></td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No recent messages</div>
{{end}}
{{end}}
`,

	"users": `
{{define "content"}}
<h1>All Users</h1>
{{if .Users}}
<table>
    <thead>
        <tr>
            <th><a href="?sort=username&order={{if eq .SortBy "username"}}{{.NextOrder}}{{else}}asc{{end}}">Username {{if eq .SortBy "username"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=email&order={{if eq .SortBy "email"}}{{.NextOrder}}{{else}}asc{{end}}">Email {{if eq .SortBy "email"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=message_count&order={{if eq .SortBy "message_count"}}{{.NextOrder}}{{else}}desc{{end}}">Messages {{if eq .SortBy "message_count"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=unique_contacts&order={{if eq .SortBy "unique_contacts"}}{{.NextOrder}}{{else}}desc{{end}}">Contacts {{if eq .SortBy "unique_contacts"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=chat_count&order={{if eq .SortBy "chat_count"}}{{.NextOrder}}{{else}}desc{{end}}">Chats {{if eq .SortBy "chat_count"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=created_at&order={{if eq .SortBy "created_at"}}{{.NextOrder}}{{else}}desc{{end}}">Created {{if eq .SortBy "created_at"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=last_seen&order={{if eq .SortBy "last_seen"}}{{.NextOrder}}{{else}}desc{{end}}">Last Seen {{if eq .SortBy "last_seen"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th>Actions</th>
        </tr>
    </thead>
    <tbody>
        {{range .Users}}
        <tr>
            <td><a href="/admin/users/{{.ID}}">{{.Username}}</a></td>
            <td>{{.Email}}</td>
            <td>{{.MessageCount}}</td>
            <td>{{.UniqueContacts}}</td>
            <td>{{.ChatCount}}</td>
            <td class="meta">{{formatTime .CreatedAt}}</td>
            <td class="meta">{{if .LastSeen}}{{formatTime .LastSeen}}{{else}}Never{{end}}</td>
            <td>
                <a href="/admin/users/{{.ID}}" class="btn">View</a>
            </td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No users found</div>
{{end}}
{{end}}
`,

	"user_detail": `
{{define "content"}}
<h1>{{.User.Username}}</h1>
<p class="meta">ID: {{.User.ID}} | Email: {{.User.Email}} | Created: {{formatTime .User.CreatedAt}}</p>

<div class="stats" style="margin: 20px 0;">
    <div class="stat-card">
        <div class="number">{{.UniquePartners}}</div>
        <div class="label">Unique Contacts</div>
    </div>
    <div class="stat-card">
        <div class="number">{{.SentMessageCount}}</div>
        <div class="label">Messages Sent</div>
    </div>
    <div class="stat-card">
        <div class="number">{{len .Chats}}</div>
        <div class="label">Total Chats</div>
    </div>
    <div class="stat-card">
        <div class="number">{{len .Messages}}</div>
        <div class="label">Recent Messages</div>
    </div>
</div>

<button class="btn btn-danger" onclick="deleteUser('{{.User.ID}}')" style="margin: 20px 0;">Delete User</button>

<h2>Communication Partners</h2>
{{if .CommunicationPartners}}
<table>
    <thead><tr><th>User</th><th>Email</th><th>Shared Chats</th><th>Messages</th><th>Last Activity</th></tr></thead>
    <tbody>
        {{range .CommunicationPartners}}
        <tr>
            <td><a href="/admin/users/{{.UserID}}">{{.Username}}</a></td>
            <td class="meta">{{.Email}}</td>
            <td>{{.ChatCount}}</td>
            <td>{{.MessageCount}}</td>
            <td class="meta">{{if .LastMessage}}{{formatTime .LastMessage}}{{else}}Never{{end}}</td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No communication partners</div>
{{end}}

<h2>Devices ({{len .Devices}})</h2>
{{if .Devices}}
<table>
    <thead><tr><th>Name</th><th>Created</th><th>Last Used</th></tr></thead>
    <tbody>
        {{range .Devices}}
        <tr>
            <td>{{.DeviceName}}</td>
            <td class="meta">{{formatTime .CreatedAt}}</td>
            <td class="meta">{{formatTime .LastUsed}}</td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No devices</div>
{{end}}

<h2>All Chats ({{len .Chats}})</h2>
{{if .Chats}}
<table>
    <thead><tr><th>Chat</th><th>Status</th><th>Type</th></tr></thead>
    <tbody>
        {{range .Chats}}
        <tr>
            <td><a href="/admin/chats/{{.ID}}">{{if .Name}}{{.Name}}{{else}}{{shortID .ID}}{{end}}</a></td>
            <td>
                {{if eq .Status "active"}}<span class="badge">Active</span>
                {{else if eq .Status "orphaned"}}<span class="badge badge-orphaned">Orphaned</span>
                {{else if eq .Status "self_destructed"}}<span class="badge badge-destructed">Self-Destructed</span>
                {{else}}<span class="badge">{{.Status}}</span>{{end}}
            </td>
            <td>{{if .IsGroup}}<span class="badge badge-group">Group</span>{{else}}<span class="badge">DM</span>{{end}}</td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No chats</div>
{{end}}

<h2>Conversations Involving This User</h2>
<p class="meta">All chats where {{.User.Username}} is or was a participant</p>
{{if .ConversationsInvolving}}
<table>
    <thead><tr><th>Chat</th><th>Status</th><th>Participants</th><th>Messages</th></tr></thead>
    <tbody>
        {{range .ConversationsInvolving}}
        <tr>
            <td><a href="/admin/chats/{{.ChatID}}">{{if .ChatName}}{{.ChatName}}{{else}}{{shortID .ChatID}}{{end}}</a></td>
            <td>
                {{if eq .Status "active"}}<span class="badge">Active</span>
                {{else if eq .Status "orphaned"}}<span class="badge badge-orphaned">Orphaned</span>
                {{else if eq .Status "self_destructed"}}<span class="badge badge-destructed">Self-Destructed</span>
                {{else}}<span class="badge">{{.Status}}</span>{{end}}
            </td>
            <td>
                {{if .ParticipantNames}}{{.ParticipantNames}}{{end}}
                {{if .FormerParticipants}}<span class="meta"> | Former: {{.FormerParticipants}}</span>{{end}}
            </td>
            <td>{{.MessageCount}}</td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No conversations found</div>
{{end}}

<h2>Recent Messages</h2>
{{if .Messages}}
<table>
    <thead><tr><th>Time</th><th>Chat</th><th>Content</th><th>Actions</th></tr></thead>
    <tbody>
        {{range .Messages}}
        <tr>
            <td class="meta">{{formatTime .CreatedAt}}</td>
            <td><a href="/admin/chats/{{.ChatID}}">{{shortID .ChatID}}</a></td>
            <td>{{truncate .Content 60}}</td>
            <td><button class="btn btn-danger" onclick="deleteMessage('{{.ID}}')">Delete</button></td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No messages</div>
{{end}}
{{end}}
`,

	"chats": `
{{define "content"}}
<h1>All Chats</h1>

<form class="search-box" method="GET" style="flex-wrap: wrap;">
    <select name="status" style="padding: 12px; background: #16213e; color: #fff; border: none; border-radius: 4px;">
        <option value="">All Statuses</option>
        <option value="active" {{if eq .StatusFilter "active"}}selected{{end}}>Active</option>
        <option value="orphaned" {{if eq .StatusFilter "orphaned"}}selected{{end}}>Orphaned</option>
        <option value="self_destructed" {{if eq .StatusFilter "self_destructed"}}selected{{end}}>Self-Destructed</option>
    </select>
    <input type="text" name="participant" placeholder="Participant name..." value="{{.ParticipantFilter}}" style="flex: 1; min-width: 150px;">
    <input type="text" name="q" placeholder="Search messages..." value="{{.MessageQuery}}" style="flex: 1; min-width: 150px;">
    <button type="submit" class="btn">Filter</button>
    {{if or .StatusFilter .ParticipantFilter .MessageQuery}}
    <a href="/admin/chats" class="btn" style="background: #666;">Clear</a>
    {{end}}
</form>

{{if .Chats}}
<table>
    <thead>
        <tr>
            <th><a href="?sort=name&order={{if eq .SortBy "name"}}{{.NextOrder}}{{else}}asc{{end}}{{if .StatusFilter}}&status={{.StatusFilter}}{{end}}{{if .ParticipantFilter}}&participant={{.ParticipantFilter}}{{end}}{{if .MessageQuery}}&q={{.MessageQuery}}{{end}}">Name {{if eq .SortBy "name"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=status&order={{if eq .SortBy "status"}}{{.NextOrder}}{{else}}asc{{end}}{{if .StatusFilter}}&status={{.StatusFilter}}{{end}}{{if .ParticipantFilter}}&participant={{.ParticipantFilter}}{{end}}{{if .MessageQuery}}&q={{.MessageQuery}}{{end}}">Status {{if eq .SortBy "status"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=is_group&order={{if eq .SortBy "is_group"}}{{.NextOrder}}{{else}}desc{{end}}{{if .StatusFilter}}&status={{.StatusFilter}}{{end}}{{if .ParticipantFilter}}&participant={{.ParticipantFilter}}{{end}}{{if .MessageQuery}}&q={{.MessageQuery}}{{end}}">Type {{if eq .SortBy "is_group"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=participant_count&order={{if eq .SortBy "participant_count"}}{{.NextOrder}}{{else}}desc{{end}}{{if .StatusFilter}}&status={{.StatusFilter}}{{end}}{{if .ParticipantFilter}}&participant={{.ParticipantFilter}}{{end}}{{if .MessageQuery}}&q={{.MessageQuery}}{{end}}">Participants {{if eq .SortBy "participant_count"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th>Former</th>
            <th><a href="?sort=message_count&order={{if eq .SortBy "message_count"}}{{.NextOrder}}{{else}}desc{{end}}{{if .StatusFilter}}&status={{.StatusFilter}}{{end}}{{if .ParticipantFilter}}&participant={{.ParticipantFilter}}{{end}}{{if .MessageQuery}}&q={{.MessageQuery}}{{end}}">Messages {{if eq .SortBy "message_count"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=created_at&order={{if eq .SortBy "created_at"}}{{.NextOrder}}{{else}}desc{{end}}{{if .StatusFilter}}&status={{.StatusFilter}}{{end}}{{if .ParticipantFilter}}&participant={{.ParticipantFilter}}{{end}}{{if .MessageQuery}}&q={{.MessageQuery}}{{end}}">Created {{if eq .SortBy "created_at"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
            <th><a href="?sort=last_message_at&order={{if eq .SortBy "last_message_at"}}{{.NextOrder}}{{else}}desc{{end}}{{if .StatusFilter}}&status={{.StatusFilter}}{{end}}{{if .ParticipantFilter}}&participant={{.ParticipantFilter}}{{end}}{{if .MessageQuery}}&q={{.MessageQuery}}{{end}}">Last Activity {{if eq .SortBy "last_message_at"}}{{if eq .Order "asc"}}↑{{else}}↓{{end}}{{end}}</a></th>
        </tr>
    </thead>
    <tbody>
        {{range .Chats}}
        <tr>
            <td><a href="/admin/chats/{{.ID}}">{{if .Name}}{{.Name}}{{else}}{{shortID .ID}}{{end}}</a></td>
            <td>
                {{if eq .Status "active"}}<span class="badge">Active</span>
                {{else if eq .Status "orphaned"}}<span class="badge badge-orphaned">Orphaned</span>
                {{else if eq .Status "self_destructed"}}<span class="badge badge-destructed">Self-Destructed</span>
                {{else}}<span class="badge">{{.Status}}</span>{{end}}
            </td>
            <td>{{if .IsGroup}}<span class="badge badge-group">Group</span>{{else}}<span class="badge">DM</span>{{end}}</td>
            <td>{{range $i, $p := .Participants}}{{if $i}}, {{end}}{{$p}}{{end}}{{if not .Participants}}<span class="meta">None</span>{{end}}</td>
            <td class="meta">{{range $i, $p := .FormerParticipants}}{{if $i}}, {{end}}{{$p}}{{end}}</td>
            <td>{{.MessageCount}}</td>
            <td class="meta">{{formatTime .CreatedAt}}</td>
            <td class="meta">{{if .LastMessageAt}}{{formatTime .LastMessageAt}}{{else}}No messages{{end}}</td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No chats found</div>
{{end}}
{{end}}
`,

	"chat_detail": `
{{define "content"}}
<h1>{{if .Chat.Name}}{{.Chat.Name}}{{else}}Chat {{shortID .Chat.ID}}{{end}}</h1>
<p class="meta">
    ID: {{.Chat.ID}} |
    {{if eq .ChatStatus "active"}}<span class="badge">Active</span>
    {{else if eq .ChatStatus "orphaned"}}<span class="badge badge-orphaned">Orphaned</span>
    {{else if eq .ChatStatus "self_destructed"}}<span class="badge badge-destructed">Self-Destructed</span>
    {{else}}<span class="badge">{{.ChatStatus}}</span>{{end}} |
    {{if .Chat.IsGroup}}<span class="badge badge-group">Group</span>{{else}}<span class="badge">DM</span>{{end}} |
    {{.TotalCount}} messages |
    Created: {{formatTime .Chat.CreatedAt}}
</p>

<h2>Active Participants ({{len .ActiveParticipants}})</h2>
{{if .ActiveParticipants}}
<table>
    <thead><tr><th>User</th><th>Joined</th></tr></thead>
    <tbody>
        {{range .ActiveParticipants}}
        <tr>
            <td><a href="/admin/users/{{.ID}}">{{.Username}}</a></td>
            <td class="meta">{{formatTime .JoinedAt}}</td>
        </tr>
        {{end}}
    </tbody>
</table>
{{else}}
<div class="empty">No active participants</div>
{{end}}

{{if .FormerParticipants}}
<h2>Former Participants ({{len .FormerParticipants}})</h2>
<table>
    <thead><tr><th>User</th><th>Status</th><th>Joined</th><th>Left</th></tr></thead>
    <tbody>
        {{range .FormerParticipants}}
        <tr>
            <td><a href="/admin/users/{{.ID}}">{{.Username}}</a></td>
            <td>
                {{if eq .Status "left"}}<span class="badge badge-orphaned">Left</span>
                {{else if eq .Status "self_destructed"}}<span class="badge badge-destructed">Self-Destructed</span>
                {{else}}<span class="badge">{{.Status}}</span>{{end}}
            </td>
            <td class="meta">{{formatTime .JoinedAt}}</td>
            <td class="meta">{{if .LeftAt}}{{formatTime .LeftAt}}{{end}}</td>
        </tr>
        {{end}}
    </tbody>
</table>
{{end}}

<h2>Messages</h2>
{{if .Messages}}
<table>
    <thead>
        <tr>
            <th>Time</th>
            <th>Sender</th>
            <th>Content</th>
            <th>Actions</th>
        </tr>
    </thead>
    <tbody>
        {{range .Messages}}
        <tr>
            <td class="meta">{{formatTime .CreatedAt}}</td>
            <td><a href="/admin/users/{{.SenderID}}">{{index $.UserMap .SenderID.String}}</a></td>
            <td>
                <div class="message-content">{{.Content}}</div>
            </td>
            <td><button class="btn btn-danger" onclick="deleteMessage('{{.ID}}')">Delete</button></td>
        </tr>
        {{end}}
    </tbody>
</table>

<div class="pagination">
    {{if .HasPrev}}<a href="?offset={{.PrevOffset}}&limit={{.Limit}}" class="btn">Previous</a>{{end}}
    <span class="meta">Showing {{.Offset}} - {{min (add .Offset .Limit) .TotalCount}} of {{.TotalCount}}</span>
    {{if .HasMore}}<a href="?offset={{.NextOffset}}&limit={{.Limit}}" class="btn">Next</a>{{end}}
</div>
{{else}}
<div class="empty">No messages in this chat</div>
{{end}}
{{end}}
`,

	"messages": `
{{define "content"}}
<h1>All Messages</h1>
{{if .Messages}}
<table>
    <thead>
        <tr>
            <th>Time</th>
            <th>Sender</th>
            <th>Chat</th>
            <th>Content</th>
            <th>Actions</th>
        </tr>
    </thead>
    <tbody>
        {{range .Messages}}
        <tr>
            <td class="meta">{{formatTime .CreatedAt}}</td>
            <td><a href="/admin/users/{{.SenderID}}">{{index $.UserMap .SenderID.String}}</a></td>
            <td><a href="/admin/chats/{{.ChatID}}">{{shortID .ChatID}}</a></td>
            <td>{{truncate .Content 60}}</td>
            <td><button class="btn btn-danger" onclick="deleteMessage('{{.ID}}')">Delete</button></td>
        </tr>
        {{end}}
    </tbody>
</table>

<div class="pagination">
    {{if .HasPrev}}<a href="?offset={{.PrevOffset}}&limit={{.Limit}}" class="btn">Previous</a>{{end}}
    {{if .HasMore}}<a href="?offset={{.NextOffset}}&limit={{.Limit}}" class="btn">Next</a>{{end}}
</div>
{{else}}
<div class="empty">No messages found</div>
{{end}}
{{end}}
`,

	"search": `
{{define "content"}}
<h1>Search Messages</h1>
<form class="search-box" method="GET">
    <input type="text" name="q" placeholder="Search message content..." value="{{.Query}}" autofocus>
    <button type="submit" class="btn">Search</button>
</form>

{{if .Query}}
    {{if .Messages}}
    <p class="meta">Found {{len .Messages}} results for "{{.Query}}"</p>
    <table>
        <thead>
            <tr>
                <th>Time</th>
                <th>Sender</th>
                <th>Chat</th>
                <th>Content</th>
            </tr>
        </thead>
        <tbody>
            {{range .Messages}}
            <tr>
                <td class="meta">{{formatTime .CreatedAt}}</td>
                <td><a href="/admin/users/{{.SenderID}}">{{index $.UserMap .SenderID.String}}</a></td>
                <td><a href="/admin/chats/{{.ChatID}}">{{shortID .ChatID}}</a></td>
                <td>
                    <div class="message-content">{{.Content}}</div>
                </td>
            </tr>
            {{end}}
        </tbody>
    </table>
    {{else}}
    <div class="empty">No messages found matching "{{.Query}}"</div>
    {{end}}
{{else}}
<div class="empty">Enter a search term to find messages</div>
{{end}}
{{end}}
`,
}
