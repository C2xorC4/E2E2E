import 'package:equatable/equatable.dart';

enum MessageStatus { sending, sent, delivered, read, failed }

class Message extends Equatable {
  final String id;
  final String chatId;
  final String senderId;
  final String content;
  final String contentType;
  final DateTime createdAt;
  final DateTime? editedAt;
  final MessageStatus status;
  final String? encryptedContent;
  final String? nonce;

  const Message({
    required this.id,
    required this.chatId,
    required this.senderId,
    required this.content,
    this.contentType = 'text',
    required this.createdAt,
    this.editedAt,
    this.status = MessageStatus.sent,
    this.encryptedContent,
    this.nonce,
  });

  bool get isEncrypted => encryptedContent != null && nonce != null;

  factory Message.fromJson(Map<String, dynamic> json) {
    return Message(
      id: json['id'] as String,
      chatId: json['chat_id'] as String,
      senderId: json['sender_id'] as String,
      content: json['content'] as String? ?? '',
      contentType: json['content_type'] as String? ?? 'text',
      createdAt: DateTime.parse(json['created_at'] as String),
      editedAt: json['edited_at'] != null
          ? DateTime.parse(json['edited_at'] as String)
          : null,
      encryptedContent: json['encrypted_content'] as String?,
      nonce: json['nonce'] as String?,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'chat_id': chatId,
        'sender_id': senderId,
        'content': content,
        'content_type': contentType,
        'created_at': createdAt.toIso8601String(),
        'edited_at': editedAt?.toIso8601String(),
      };

  Message copyWith({
    String? id,
    String? chatId,
    String? senderId,
    String? content,
    String? contentType,
    DateTime? createdAt,
    DateTime? editedAt,
    MessageStatus? status,
    String? encryptedContent,
    String? nonce,
  }) {
    return Message(
      id: id ?? this.id,
      chatId: chatId ?? this.chatId,
      senderId: senderId ?? this.senderId,
      content: content ?? this.content,
      contentType: contentType ?? this.contentType,
      createdAt: createdAt ?? this.createdAt,
      editedAt: editedAt ?? this.editedAt,
      status: status ?? this.status,
      encryptedContent: encryptedContent ?? this.encryptedContent,
      nonce: nonce ?? this.nonce,
    );
  }

  @override
  List<Object?> get props => [id, chatId, senderId, content, createdAt];
}
