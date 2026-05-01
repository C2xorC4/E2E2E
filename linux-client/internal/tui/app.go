package tui

import (
	"fmt"
	"strings"
	"time"

	"e2e-client/internal/api"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type View int

const (
	ViewChatList View = iota
	ViewChat
	ViewNewChat
	ViewSearch
	ViewGroupCreate
	ViewInviteUser
	ViewPendingInvites
)

type Model struct {
	client         *api.Client
	width          int
	height         int
	view           View
	chats          []api.ChatListItem
	selectedChat   int
	messages       []api.Message
	users          map[string]api.User
	input          textinput.Model
	viewport       viewport.Model
	searchInput    textinput.Model
	searchResults  []api.User
	selectedUser   int        // Index of selected user in search results
	selectedUsers  []api.User // Users selected for group chat
	groupNameInput textinput.Model
	pendingInvites []api.ChatInvite
	selectedInvite int
	msgChan        chan api.Message
	inviteChan     chan api.ChatInvite
	err            error
	loading        bool
	connected      bool
}

type tickMsg time.Time
type chatListMsg []api.ChatListItem
type messagesMsg []api.Message
type newMessageMsg api.Message
type newInviteMsg api.ChatInvite
type searchResultsMsg []api.User
type invitesMsg []api.ChatInvite
type connectedMsg bool
type errMsg error

func NewModel(client *api.Client) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.CharLimit = 2000
	ti.Width = 60

	si := textinput.New()
	si.Placeholder = "Search users..."
	si.CharLimit = 100
	si.Width = 40

	gi := textinput.New()
	gi.Placeholder = "Group name..."
	gi.CharLimit = 100
	gi.Width = 40

	vp := viewport.New(80, 20)

	return Model{
		client:         client,
		input:          ti,
		searchInput:    si,
		groupNameInput: gi,
		viewport:       vp,
		users:          make(map[string]api.User),
		view:           ViewChatList,
		selectedUsers:  make([]api.User, 0),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.connectWS(),
		m.loadChats(),
		m.loadInvites(),
		m.tick(),
	)
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(time.Second*5, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) connectWS() tea.Cmd {
	return func() tea.Msg {
		if err := m.client.ConnectWS(); err != nil {
			return connectedMsg(false)
		}
		return connectedMsg(true)
	}
}

func (m Model) loadChats() tea.Cmd {
	return func() tea.Msg {
		chats, err := m.client.GetChats()
		if err != nil {
			return errMsg(err)
		}
		return chatListMsg(chats)
	}
}

func (m Model) loadMessages(chatID string) tea.Cmd {
	return func() tea.Msg {
		messages, err := m.client.GetMessages(chatID, 50, 0)
		if err != nil {
			return errMsg(err)
		}
		return messagesMsg(messages)
	}
}

func (m Model) searchUsers(query string) tea.Cmd {
	return func() tea.Msg {
		users, err := m.client.SearchUsers(query)
		if err != nil {
			return errMsg(err)
		}
		return searchResultsMsg(users)
	}
}

func (m Model) loadInvites() tea.Cmd {
	return func() tea.Msg {
		invites, err := m.client.GetPendingInvites()
		if err != nil {
			return errMsg(err)
		}
		return invitesMsg(invites)
	}
}

func (m Model) listenForMessages() tea.Cmd {
	return func() tea.Msg {
		if m.msgChan == nil {
			return nil
		}
		msg := <-m.msgChan
		return newMessageMsg(msg)
	}
}

func (m Model) listenForInvites() tea.Cmd {
	return func() tea.Msg {
		if m.inviteChan == nil {
			return nil
		}
		invite := <-m.inviteChan
		return newInviteMsg(invite)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle key events - may return early for special keys
		newModel, cmd, handled := m.handleKey(msg)
		if handled {
			return newModel, cmd
		}
		m = newModel

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 4
		m.viewport.Height = msg.Height - 10
		m.input.Width = msg.Width - 6

	case tickMsg:
		cmds = append(cmds, m.tick())
		if m.connected && m.msgChan != nil {
			cmds = append(cmds, m.listenForMessages())
		}
		if m.connected && m.inviteChan != nil {
			cmds = append(cmds, m.listenForInvites())
		}

	case connectedMsg:
		m.connected = bool(msg)
		if m.connected {
			m.msgChan = m.client.Subscribe()
			m.inviteChan = m.client.SubscribeInvites()
			cmds = append(cmds, m.listenForMessages(), m.listenForInvites())
		}

	case chatListMsg:
		m.chats = []api.ChatListItem(msg)
		m.loading = false
		// Build user map
		for _, chat := range m.chats {
			for _, user := range chat.Participants {
				m.users[user.ID] = user
			}
		}

	case messagesMsg:
		m.messages = []api.Message(msg)
		m.loading = false
		m.updateViewport()

	case newMessageMsg:
		message := api.Message(msg)
		if m.view == ViewChat && len(m.chats) > 0 && m.chats[m.selectedChat].Chat.ID == message.ChatID {
			m.messages = append([]api.Message{message}, m.messages...)
			m.updateViewport()
		}
		cmds = append(cmds, m.loadChats(), m.listenForMessages())

	case searchResultsMsg:
		m.searchResults = []api.User(msg)
		m.loading = false

	case invitesMsg:
		m.pendingInvites = []api.ChatInvite(msg)
		m.loading = false

	case newInviteMsg:
		// A new invite arrived via WebSocket - add to list and refresh
		invite := api.ChatInvite(msg)
		m.pendingInvites = append([]api.ChatInvite{invite}, m.pendingInvites...)
		cmds = append(cmds, m.listenForInvites())

	case errMsg:
		m.err = msg
		m.loading = false
	}

	// Update sub-components
	if m.view == ViewChat {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.view == ViewNewChat || m.view == ViewSearch || m.view == ViewInviteUser {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.view == ViewGroupCreate {
		var cmd tea.Cmd
		m.groupNameInput, cmd = m.groupNameInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	key := msg.String()

	// If input is focused, only handle special keys
	if m.input.Focused() && m.view == ViewChat {
		switch key {
		case "ctrl+c":
			m.client.DisconnectWS()
			return m, tea.Quit, true
		case "esc":
			m.input.Blur()
			return m, nil, true
		case "enter":
			if m.input.Value() != "" {
				content := m.input.Value()
				chatID := m.chats[m.selectedChat].Chat.ID
				m.input.SetValue("")

				// Add message to local list immediately (optimistic update)
				newMsg := api.Message{
					ID:          fmt.Sprintf("local-%d", time.Now().UnixNano()),
					ChatID:      chatID,
					SenderID:    m.client.GetConfig().UserID,
					Content:     content,
					ContentType: "text",
					CreatedAt:   time.Now(),
				}
				m.messages = append([]api.Message{newMsg}, m.messages...)
				m.updateViewport()

				// Send to server in background
				go m.client.SendMessage(chatID, content)
			}
			return m, nil, true
		case "tab":
			m.input.Blur()
			return m, nil, true
		default:
			// Let the input handle all other keys
			return m, nil, false
		}
	}

	// If search input is focused
	if m.searchInput.Focused() && (m.view == ViewNewChat || m.view == ViewSearch || m.view == ViewInviteUser) {
		switch key {
		case "ctrl+c":
			m.client.DisconnectWS()
			return m, tea.Quit, true
		case "esc":
			m.view = ViewChatList
			m.searchInput.Blur()
			m.searchInput.SetValue("")
			m.searchResults = nil
			m.selectedUser = 0
			m.selectedUsers = nil
			return m, nil, true
		case "enter":
			if m.searchInput.Value() != "" {
				m.loading = true
				return m, m.searchUsers(m.searchInput.Value()), true
			}
			// Select from results if available
			if len(m.searchResults) > 0 && m.selectedUser < len(m.searchResults) {
				user := m.searchResults[m.selectedUser]

				if m.view == ViewInviteUser {
					// Invite to existing chat
					if len(m.chats) > 0 {
						chatID := m.chats[m.selectedChat].Chat.ID
						err := m.client.InviteToChat(chatID, user.ID)
						if err == nil {
							m.view = ViewChat
							m.searchInput.SetValue("")
							m.searchResults = nil
							m.selectedUser = 0
							return m, m.loadChats(), true
						} else {
							m.err = err
						}
					}
				} else {
					// Create new chat
					chat, err := m.client.CreateChat([]string{user.ID}, false, "")
					if err == nil {
						m.view = ViewChatList
						m.searchInput.SetValue("")
						m.searchResults = nil
						m.selectedUser = 0
						_ = chat
						return m, m.loadChats(), true
					} else {
						m.err = err
					}
				}
			}
			return m, nil, true
		case "up":
			if len(m.searchResults) > 0 && m.selectedUser > 0 {
				m.selectedUser--
			}
			return m, nil, true
		case "down":
			if len(m.searchResults) > 0 && m.selectedUser < len(m.searchResults)-1 {
				m.selectedUser++
			}
			return m, nil, true
		case "tab":
			// Toggle user selection for group chat
			if len(m.searchResults) > 0 && m.selectedUser < len(m.searchResults) && m.view == ViewNewChat {
				user := m.searchResults[m.selectedUser]
				found := false
				for i, u := range m.selectedUsers {
					if u.ID == user.ID {
						// Remove from selection
						m.selectedUsers = append(m.selectedUsers[:i], m.selectedUsers[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					m.selectedUsers = append(m.selectedUsers, user)
				}
			}
			return m, nil, true
		case "ctrl+g":
			// Create group from selected users
			if len(m.selectedUsers) > 1 {
				m.view = ViewGroupCreate
				m.groupNameInput.Focus()
				return m, textinput.Blink, true
			}
			return m, nil, true
		default:
			// Let the search input handle all other keys
			return m, nil, false
		}
	}

	// If group name input is focused
	if m.groupNameInput.Focused() && m.view == ViewGroupCreate {
		switch key {
		case "ctrl+c":
			m.client.DisconnectWS()
			return m, tea.Quit, true
		case "esc":
			m.view = ViewNewChat
			m.groupNameInput.Blur()
			m.groupNameInput.SetValue("")
			m.searchInput.Focus()
			return m, textinput.Blink, true
		case "enter":
			if m.groupNameInput.Value() != "" && len(m.selectedUsers) > 0 {
				userIDs := make([]string, len(m.selectedUsers))
				for i, u := range m.selectedUsers {
					userIDs[i] = u.ID
				}
				chat, err := m.client.CreateChat(userIDs, true, m.groupNameInput.Value())
				if err == nil {
					m.view = ViewChatList
					m.groupNameInput.SetValue("")
					m.searchInput.SetValue("")
					m.searchResults = nil
					m.selectedUsers = nil
					m.selectedUser = 0
					_ = chat
					return m, m.loadChats(), true
				} else {
					m.err = err
				}
			}
			return m, nil, true
		default:
			return m, nil, false
		}
	}

	// Handle keys when no input is focused
	switch key {
	case "ctrl+c", "q":
		if m.view == ViewChatList {
			m.client.DisconnectWS()
			return m, tea.Quit, true
		}
		m.view = ViewChatList
		m.input.Blur()
		return m, nil, true

	case "esc":
		if m.view != ViewChatList {
			m.view = ViewChatList
			m.input.Blur()
			m.searchInput.Blur()
		}
		return m, nil, true

	case "n":
		if m.view == ViewChatList {
			m.view = ViewNewChat
			m.searchInput.Focus()
			m.searchResults = nil
			return m, textinput.Blink, true
		}

	case "r":
		if m.view == ViewChatList {
			m.loading = true
			return m, m.loadChats(), true
		}

	case "p":
		if m.view == ViewChatList {
			m.view = ViewPendingInvites
			m.loading = true
			m.selectedInvite = 0
			return m, m.loadInvites(), true
		}

	case "y":
		// Accept invite
		if m.view == ViewPendingInvites && len(m.pendingInvites) > 0 {
			invite := m.pendingInvites[m.selectedInvite]
			if err := m.client.AcceptInvite(invite.Chat.ID); err == nil {
				// Remove from list
				m.pendingInvites = append(m.pendingInvites[:m.selectedInvite], m.pendingInvites[m.selectedInvite+1:]...)
				if m.selectedInvite >= len(m.pendingInvites) && m.selectedInvite > 0 {
					m.selectedInvite--
				}
				return m, m.loadChats(), true
			}
		}
		return m, nil, true

	case "d":
		// Decline invite
		if m.view == ViewPendingInvites && len(m.pendingInvites) > 0 {
			invite := m.pendingInvites[m.selectedInvite]
			if err := m.client.DeclineInvite(invite.Chat.ID); err == nil {
				// Remove from list
				m.pendingInvites = append(m.pendingInvites[:m.selectedInvite], m.pendingInvites[m.selectedInvite+1:]...)
				if m.selectedInvite >= len(m.pendingInvites) && m.selectedInvite > 0 {
					m.selectedInvite--
				}
			}
		}
		return m, nil, true

	case "up", "k":
		if m.view == ViewChatList && m.selectedChat > 0 {
			m.selectedChat--
		} else if m.view == ViewChat && !m.input.Focused() {
			m.viewport.LineUp(1)
		} else if m.view == ViewPendingInvites && m.selectedInvite > 0 {
			m.selectedInvite--
		}
		return m, nil, true

	case "down", "j":
		if m.view == ViewChatList && m.selectedChat < len(m.chats)-1 {
			m.selectedChat++
		} else if m.view == ViewChat && !m.input.Focused() {
			m.viewport.LineDown(1)
		} else if m.view == ViewPendingInvites && m.selectedInvite < len(m.pendingInvites)-1 {
			m.selectedInvite++
		}
		return m, nil, true

	case "enter":
		newModel, cmd := m.handleEnter()
		return newModel, cmd, true

	case "tab", "i":
		if m.view == ViewChat {
			m.input.Focus()
			return m, textinput.Blink, true
		}

	case "a":
		// Add/invite user to group chat
		if m.view == ViewChat && len(m.chats) > 0 {
			chat := m.chats[m.selectedChat]
			if chat.Chat.IsGroup {
				m.view = ViewInviteUser
				m.searchInput.Focus()
				m.searchResults = nil
				m.selectedUser = 0
				return m, textinput.Blink, true
			}
		}

	case "l":
		// Leave chat
		if m.view == ViewChat && len(m.chats) > 0 {
			chatID := m.chats[m.selectedChat].Chat.ID
			if err := m.client.LeaveChat(chatID); err == nil {
				m.view = ViewChatList
				m.messages = nil
				return m, m.loadChats(), true
			} else {
				m.err = err
			}
		}
		return m, nil, true

	case "x":
		// Self-destruct chat
		if m.view == ViewChat && len(m.chats) > 0 {
			chatID := m.chats[m.selectedChat].Chat.ID
			if err := m.client.SelfDestructChat(chatID); err == nil {
				m.view = ViewChatList
				m.messages = nil
				return m, m.loadChats(), true
			} else {
				m.err = err
			}
		}
		return m, nil, true
	}

	return m, nil, true
}

func (m Model) handleEnter() (Model, tea.Cmd) {
	switch m.view {
	case ViewChatList:
		if len(m.chats) > 0 {
			m.view = ViewChat
			m.input.Focus()
			m.loading = true
			return m, tea.Batch(m.loadMessages(m.chats[m.selectedChat].Chat.ID), textinput.Blink)
		}

	case ViewChat:
		if m.input.Value() != "" {
			content := m.input.Value()
			chatID := m.chats[m.selectedChat].Chat.ID
			m.input.SetValue("")
			go m.client.SendMessage(chatID, content)
		}

	case ViewNewChat:
		if m.searchInput.Value() != "" {
			m.loading = true
			return m, m.searchUsers(m.searchInput.Value())
		}

	case ViewSearch:
		// Select user from search results
		// For simplicity, create chat with first result
		if len(m.searchResults) > 0 {
			user := m.searchResults[0]
			chat, err := m.client.CreateChat([]string{user.ID}, false, "")
			if err == nil {
				m.view = ViewChatList
				m.searchInput.SetValue("")
				m.searchResults = nil
				_ = chat
				return m, m.loadChats()
			}
		}
	}

	return m, nil
}

func (m *Model) updateViewport() {
	var sb strings.Builder
	// Messages are newest first, display oldest first
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		isOwn := msg.SenderID == m.client.GetConfig().UserID

		sender := "Unknown"
		if user, ok := m.users[msg.SenderID]; ok {
			sender = user.Username
		}
		if isOwn {
			sender = "You"
		}

		timeStr := msg.CreatedAt.Format("15:04")

		var msgStyle lipgloss.Style
		if isOwn {
			msgStyle = OwnMessageStyle
		} else {
			msgStyle = OtherMessageStyle
		}

		header := fmt.Sprintf("%s  %s",
			MessageSenderStyle.Render(sender),
			MessageTimeStyle.Render(timeStr))

		sb.WriteString(header + "\n")
		sb.WriteString(msgStyle.Render(msg.Content) + "\n\n")
	}

	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var content string

	switch m.view {
	case ViewChatList:
		content = m.renderChatList()
	case ViewChat:
		content = m.renderChat()
	case ViewNewChat, ViewSearch:
		content = m.renderNewChat()
	case ViewGroupCreate:
		content = m.renderGroupCreate()
	case ViewInviteUser:
		content = m.renderInviteUser()
	case ViewPendingInvites:
		content = m.renderPendingInvites()
	}

	// Status bar
	status := m.renderStatusBar()

	return AppStyle.Render(content + "\n" + status)
}

func (m Model) renderChatList() string {
	var sb strings.Builder

	// Header
	header := "🔒 Secure Chat"
	if len(m.pendingInvites) > 0 {
		header += fmt.Sprintf("  📨 %d invite(s)", len(m.pendingInvites))
	}
	sb.WriteString(HeaderStyle.Render(header) + "\n\n")

	if len(m.chats) == 0 {
		sb.WriteString(HelpStyle.Render("No conversations yet. Press 'n' to start a new chat.\n"))
	}

	for i, chat := range m.chats {
		name := chat.Chat.Name
		if name == "" && len(chat.Participants) > 0 {
			for _, p := range chat.Participants {
				if p.ID != m.client.GetConfig().UserID {
					name = p.Username
					break
				}
			}
		}
		if name == "" {
			name = "Chat"
		}

		// Add group indicator
		prefix := ""
		if chat.Chat.IsGroup {
			prefix = "👥 "
		} else {
			prefix = "💬 "
		}

		preview := ""
		if chat.LastMessage != nil {
			preview = Truncate(chat.LastMessage.Content, 30)
		}

		line := fmt.Sprintf("%s%-18s %s", prefix, ChatNameStyle.Render(name), ChatPreviewStyle.Render(preview))

		if chat.UnreadCount > 0 {
			line += " " + UnreadBadgeStyle.Render(fmt.Sprintf("%d", chat.UnreadCount))
		}

		if i == m.selectedChat {
			sb.WriteString(ChatItemSelectedStyle.Render(line) + "\n")
		} else {
			sb.WriteString(ChatItemStyle.Render(line) + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(HelpStyle.Render("↑/↓: Navigate • Enter: Open • n: New chat • p: Invites • r: Refresh • q: Quit"))

	return ChatListStyle.Width(m.width - 4).Render(sb.String())
}

func (m Model) renderChat() string {
	var sb strings.Builder

	if len(m.chats) == 0 {
		return "No chat selected"
	}

	chat := m.chats[m.selectedChat]
	name := chat.Chat.Name
	if name == "" && len(chat.Participants) > 0 {
		for _, p := range chat.Participants {
			if p.ID != m.client.GetConfig().UserID {
				name = p.Username
				break
			}
		}
	}

	// Header - show group indicator
	chatIcon := "💬"
	participantInfo := ""
	if chat.Chat.IsGroup {
		chatIcon = "👥"
		participantInfo = fmt.Sprintf(" (%d members)", len(chat.Participants))
	}
	header := fmt.Sprintf("%s %s%s  %s", chatIcon, name, participantInfo, EncryptedStyle.Render("🔒 Encrypted"))
	sb.WriteString(HeaderStyle.Render(header) + "\n")

	// Messages viewport
	sb.WriteString(MessagePaneStyle.Width(m.width - 4).Height(m.height - 8).Render(m.viewport.View()) + "\n")

	// Input
	inputStyle := InputStyle
	if m.input.Focused() {
		inputStyle = InputFocusedStyle
	}
	sb.WriteString(inputStyle.Width(m.width - 4).Render(m.input.View()) + "\n")

	help := "Tab: Input • Enter: Send • l: Leave • x: Self-destruct • Esc: Back"
	if chat.Chat.IsGroup {
		help = "Tab: Input • Enter: Send • a: Add • l: Leave • x: Self-destruct • Esc: Back"
	}
	sb.WriteString(HelpStyle.Render(help))

	return sb.String()
}

func (m Model) renderNewChat() string {
	var sb strings.Builder

	sb.WriteString(HeaderStyle.Render("New Conversation") + "\n\n")

	// Show selected users for group
	if len(m.selectedUsers) > 0 {
		sb.WriteString("Selected for group:\n")
		for _, user := range m.selectedUsers {
			sb.WriteString(fmt.Sprintf("  ✓ %s\n", user.Username))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Search for a user:\n")
	sb.WriteString(InputFocusedStyle.Render(m.searchInput.View()) + "\n\n")

	if m.loading {
		sb.WriteString("Searching...\n")
	} else if len(m.searchResults) > 0 {
		sb.WriteString("Results (↑/↓ to navigate, Tab to select for group):\n")
		for i, user := range m.searchResults {
			prefix := "  "
			if i == m.selectedUser {
				prefix = "▸ "
			}
			// Check if already selected for group
			isSelected := false
			for _, su := range m.selectedUsers {
				if su.ID == user.ID {
					isSelected = true
					break
				}
			}
			marker := ""
			if isSelected {
				marker = " ✓"
			}
			line := fmt.Sprintf("%s%s (%s)%s\n", prefix, user.Username, user.Email, marker)
			if i == m.selectedUser {
				sb.WriteString(ChatItemSelectedStyle.Render(line))
			} else {
				sb.WriteString(line)
			}
		}
	}

	var help string
	if len(m.selectedUsers) > 1 {
		help = "Enter: DM selected • Ctrl+G: Create group • Tab: Toggle select • Esc: Back"
	} else {
		help = "Enter: Start DM • Tab: Select for group • ↑/↓: Navigate • Esc: Back"
	}
	sb.WriteString("\n" + HelpStyle.Render(help))

	return DialogStyle.Render(sb.String())
}

func (m Model) renderGroupCreate() string {
	var sb strings.Builder

	sb.WriteString(HeaderStyle.Render("Create Group Chat") + "\n\n")

	sb.WriteString("Members:\n")
	for _, user := range m.selectedUsers {
		sb.WriteString(fmt.Sprintf("  • %s\n", user.Username))
	}
	sb.WriteString("\nGroup name:\n")
	sb.WriteString(InputFocusedStyle.Render(m.groupNameInput.View()) + "\n\n")

	sb.WriteString(HelpStyle.Render("Enter: Create group • Esc: Back"))

	return DialogStyle.Render(sb.String())
}

func (m Model) renderInviteUser() string {
	var sb strings.Builder

	chatName := "Chat"
	if len(m.chats) > 0 && m.selectedChat < len(m.chats) {
		chat := m.chats[m.selectedChat]
		if chat.Chat.Name != "" {
			chatName = chat.Chat.Name
		}
	}

	sb.WriteString(HeaderStyle.Render(fmt.Sprintf("Invite to: %s", chatName)) + "\n\n")
	sb.WriteString("Search for a user to invite:\n")
	sb.WriteString(InputFocusedStyle.Render(m.searchInput.View()) + "\n\n")

	if m.loading {
		sb.WriteString("Searching...\n")
	} else if len(m.searchResults) > 0 {
		sb.WriteString("Results:\n")
		for i, user := range m.searchResults {
			prefix := "  "
			if i == m.selectedUser {
				prefix = "▸ "
			}
			line := fmt.Sprintf("%s%s (%s)\n", prefix, user.Username, user.Email)
			if i == m.selectedUser {
				sb.WriteString(ChatItemSelectedStyle.Render(line))
			} else {
				sb.WriteString(line)
			}
		}
	}

	sb.WriteString("\n" + HelpStyle.Render("Enter: Invite • ↑/↓: Navigate • Esc: Back"))

	return DialogStyle.Render(sb.String())
}

func (m Model) renderPendingInvites() string {
	var sb strings.Builder

	sb.WriteString(HeaderStyle.Render("📨 Pending Chat Invites") + "\n\n")

	if m.loading {
		sb.WriteString("Loading invites...\n")
	} else if len(m.pendingInvites) == 0 {
		sb.WriteString(HelpStyle.Render("No pending invites.\n"))
	} else {
		for i, invite := range m.pendingInvites {
			prefix := "  "
			if i == m.selectedInvite {
				prefix = "▸ "
			}

			chatName := invite.Chat.Name
			if chatName == "" {
				chatName = "Direct Message"
			}
			chatType := "💬"
			if invite.Chat.IsGroup {
				chatType = "👥"
			}

			line := fmt.Sprintf("%s%s %s from %s", prefix, chatType, chatName, invite.InvitedBy.Username)
			if i == m.selectedInvite {
				sb.WriteString(ChatItemSelectedStyle.Render(line) + "\n")
			} else {
				sb.WriteString(line + "\n")
			}
		}
	}

	sb.WriteString("\n" + HelpStyle.Render("↑/↓: Navigate • y: Accept • d: Decline • Esc: Back"))

	return DialogStyle.Render(sb.String())
}

func (m Model) renderStatusBar() string {
	connStatus := OfflineStyle.Render("● Disconnected")
	if m.connected {
		connStatus = OnlineStyle.Render("● Connected")
	}

	user := m.client.GetConfig().Username
	if user == "" {
		user = "Not logged in"
	}

	return StatusBarStyle.Render(fmt.Sprintf("%s  |  %s  |  %s",
		connStatus,
		user,
		EncryptedStyle.Render("End-to-end encrypted")))
}
