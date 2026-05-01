import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../models/models.dart';
import '../providers/providers.dart';

class ChatListScreen extends StatefulWidget {
  const ChatListScreen({super.key});

  @override
  State<ChatListScreen> createState() => _ChatListScreenState();
}

class _ChatListScreenState extends State<ChatListScreen> {
  @override
  void initState() {
    super.initState();
    // Load pending invites when screen loads
    WidgetsBinding.instance.addPostFrameCallback((_) {
      context.read<ChatProvider>().loadPendingInvites();
    });
  }

  void _showPendingInvites() {
    final chatProvider = context.read<ChatProvider>();

    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      builder: (context) => DraggableScrollableSheet(
        initialChildSize: 0.5,
        minChildSize: 0.3,
        maxChildSize: 0.9,
        expand: false,
        builder: (context, scrollController) => _PendingInvitesSheet(
          chatProvider: chatProvider,
          scrollController: scrollController,
        ),
      ),
    );
  }

  String _formatTime(DateTime time) {
    final now = DateTime.now();
    final diff = now.difference(time);

    if (diff.inDays > 7) {
      return '${time.month}/${time.day}';
    } else if (diff.inDays > 0) {
      return '${diff.inDays}d';
    } else if (diff.inHours > 0) {
      return '${diff.inHours}h';
    } else if (diff.inMinutes > 0) {
      return '${diff.inMinutes}m';
    } else {
      return 'now';
    }
  }

  @override
  Widget build(BuildContext context) {
    final auth = context.watch<AuthProvider>();
    final chatProvider = context.watch<ChatProvider>();

    return Scaffold(
      appBar: AppBar(
        title: const Row(
          children: [
            Icon(Icons.lock, size: 20),
            SizedBox(width: 8),
            Text('E2E Chat'),
          ],
        ),
        actions: [
          Stack(
            children: [
              IconButton(
                icon: const Icon(Icons.mail_outline),
                onPressed: _showPendingInvites,
                tooltip: 'Pending Invites',
              ),
              if (chatProvider.pendingInviteCount > 0)
                Positioned(
                  right: 4,
                  top: 4,
                  child: Container(
                    padding: const EdgeInsets.all(4),
                    decoration: const BoxDecoration(
                      color: Colors.red,
                      shape: BoxShape.circle,
                    ),
                    child: Text(
                      chatProvider.pendingInviteCount.toString(),
                      style: const TextStyle(
                        color: Colors.white,
                        fontSize: 10,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                  ),
                ),
            ],
          ),
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: () => chatProvider.loadChats(),
          ),
          PopupMenuButton<String>(
            onSelected: (value) {
              if (value == 'logout') {
                auth.logout();
              }
            },
            itemBuilder: (context) => [
              PopupMenuItem<String>(
                enabled: false,
                child: ListTile(
                  leading: const Icon(Icons.person),
                  title: Text(auth.username ?? 'User'),
                  subtitle: Text(auth.email ?? ''),
                  dense: true,
                  contentPadding: EdgeInsets.zero,
                ),
              ),
              const PopupMenuDivider(),
              const PopupMenuItem<String>(
                value: 'logout',
                child: ListTile(
                  leading: Icon(Icons.logout, color: Colors.red),
                  title: Text('Logout', style: TextStyle(color: Colors.red)),
                  dense: true,
                  contentPadding: EdgeInsets.zero,
                ),
              ),
            ],
          ),
        ],
      ),
      body: chatProvider.isLoading && chatProvider.chats.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : chatProvider.chats.isEmpty
              ? Center(
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Icon(
                        Icons.chat_bubble_outline,
                        size: 64,
                        color: Colors.grey[400],
                      ),
                      const SizedBox(height: 16),
                      Text(
                        'No conversations yet',
                        style: TextStyle(
                          fontSize: 18,
                          color: Colors.grey[600],
                        ),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        'Start a new chat to begin messaging',
                        style: TextStyle(color: Colors.grey[500]),
                      ),
                    ],
                  ),
                )
              : RefreshIndicator(
                  onRefresh: () => chatProvider.loadChats(),
                  child: ListView.builder(
                    itemCount: chatProvider.chats.length,
                    itemBuilder: (context, index) {
                      final chat = chatProvider.chats[index];
                      return _ChatListTile(
                        chat: chat,
                        currentUserId: auth.userId!,
                        onTap: () async {
                          await chatProvider.selectChat(chat);
                          if (context.mounted) {
                            Navigator.pushNamed(context, '/chat');
                          }
                        },
                      );
                    },
                  ),
                ),
      floatingActionButton: FloatingActionButton(
        onPressed: () {
          Navigator.pushNamed(context, '/new-chat');
        },
        child: const Icon(Icons.add),
      ),
    );
  }
}

class _ChatListTile extends StatelessWidget {
  final Chat chat;
  final String currentUserId;
  final VoidCallback onTap;

  const _ChatListTile({
    required this.chat,
    required this.currentUserId,
    required this.onTap,
  });

  String _getDisplayName() {
    if (chat.name != null && chat.name!.isNotEmpty) {
      return chat.name!;
    }
    // For DMs, show the other participant's name
    final otherParticipants = chat.participants
        .where((p) => p.id != currentUserId)
        .toList();
    if (otherParticipants.isNotEmpty) {
      return otherParticipants.map((p) => p.username).join(', ');
    }
    return 'Chat';
  }

  @override
  Widget build(BuildContext context) {
    final lastMessage = chat.lastMessage;
    final displayName = _getDisplayName();

    return ListTile(
      leading: CircleAvatar(
        backgroundColor: Theme.of(context).colorScheme.primaryContainer,
        child: chat.isGroup
            ? const Icon(Icons.group)
            : Text(
                displayName.isNotEmpty ? displayName[0].toUpperCase() : '?',
                style: TextStyle(
                  color: Theme.of(context).colorScheme.onPrimaryContainer,
                ),
              ),
      ),
      title: Row(
        children: [
          Expanded(
            child: Text(
              displayName,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
          ),
          if (lastMessage != null)
            Text(
              _formatTime(lastMessage.createdAt),
              style: TextStyle(
                fontSize: 12,
                color: Colors.grey[600],
              ),
            ),
        ],
      ),
      subtitle: lastMessage != null
          ? Row(
              children: [
                const Icon(Icons.lock, size: 12, color: Colors.green),
                const SizedBox(width: 4),
                Expanded(
                  child: Text(
                    lastMessage.content,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: Colors.grey[600]),
                  ),
                ),
                if (chat.unreadCount > 0)
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: Theme.of(context).colorScheme.primary,
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Text(
                      chat.unreadCount.toString(),
                      style: const TextStyle(
                        color: Colors.white,
                        fontSize: 12,
                      ),
                    ),
                  ),
              ],
            )
          : const Text('No messages yet'),
      onTap: onTap,
    );
  }

  String _formatTime(DateTime time) {
    final now = DateTime.now();
    final diff = now.difference(time);

    if (diff.inDays > 7) {
      return '${time.month}/${time.day}';
    } else if (diff.inDays > 0) {
      return '${diff.inDays}d';
    } else if (diff.inHours > 0) {
      return '${diff.inHours}h';
    } else if (diff.inMinutes > 0) {
      return '${diff.inMinutes}m';
    } else {
      return 'now';
    }
  }
}

class _PendingInvitesSheet extends StatelessWidget {
  final ChatProvider chatProvider;
  final ScrollController scrollController;

  const _PendingInvitesSheet({
    required this.chatProvider,
    required this.scrollController,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Container(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              const Icon(Icons.mail_outline),
              const SizedBox(width: 8),
              const Text(
                'Pending Invites',
                style: TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                ),
              ),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.close),
                onPressed: () => Navigator.pop(context),
              ),
            ],
          ),
        ),
        const Divider(height: 1),
        Expanded(
          child: chatProvider.pendingInvites.isEmpty
              ? Center(
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Icon(
                        Icons.inbox,
                        size: 48,
                        color: Colors.grey[400],
                      ),
                      const SizedBox(height: 16),
                      Text(
                        'No pending invites',
                        style: TextStyle(color: Colors.grey[600]),
                      ),
                    ],
                  ),
                )
              : ListView.builder(
                  controller: scrollController,
                  itemCount: chatProvider.pendingInvites.length,
                  itemBuilder: (context, index) {
                    final invite = chatProvider.pendingInvites[index];
                    final chat = invite['chat'] as Map<String, dynamic>?;
                    final invitedBy = invite['invited_by'] as Map<String, dynamic>?;

                    final chatName = chat?['name'] ?? 'Direct Message';
                    final isGroup = chat?['is_group'] ?? false;
                    final inviterName = invitedBy?['username'] ?? 'Unknown';
                    final chatId = chat?['id'] as String?;

                    return ListTile(
                      leading: CircleAvatar(
                        child: Icon(isGroup ? Icons.group : Icons.person),
                      ),
                      title: Text(chatName),
                      subtitle: Text('Invited by $inviterName'),
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          IconButton(
                            icon: const Icon(Icons.check, color: Colors.green),
                            onPressed: chatId == null
                                ? null
                                : () async {
                                    final success = await chatProvider.acceptInvite(chatId);
                                    if (success && context.mounted) {
                                      ScaffoldMessenger.of(context).showSnackBar(
                                        const SnackBar(content: Text('Invite accepted')),
                                      );
                                    }
                                  },
                            tooltip: 'Accept',
                          ),
                          IconButton(
                            icon: const Icon(Icons.close, color: Colors.red),
                            onPressed: chatId == null
                                ? null
                                : () async {
                                    final success = await chatProvider.declineInvite(chatId);
                                    if (success && context.mounted) {
                                      ScaffoldMessenger.of(context).showSnackBar(
                                        const SnackBar(content: Text('Invite declined')),
                                      );
                                    }
                                  },
                            tooltip: 'Decline',
                          ),
                        ],
                      ),
                    );
                  },
                ),
        ),
      ],
    );
  }
}
