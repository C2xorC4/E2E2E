import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import 'services/services.dart';
import 'providers/providers.dart';
import 'screens/screens.dart';

void main() {
  runApp(const E2EChatApp());
}

class E2EChatApp extends StatelessWidget {
  const E2EChatApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MultiProvider(
      providers: [
        // Services
        Provider<StorageService>(
          create: (_) => StorageService(),
        ),
        Provider<CryptoService>(
          create: (_) => CryptoService(),
        ),
        ProxyProvider<StorageService, ApiService>(
          update: (_, storage, __) => ApiService(
            baseUrl: storage.getServerUrl(),
          ),
        ),
        ProxyProvider<StorageService, WebSocketService>(
          update: (_, storage, __) => WebSocketService(
            baseUrl: storage.getServerUrl(),
          ),
        ),

        // Auth Provider
        ChangeNotifierProxyProvider4<ApiService, CryptoService, StorageService,
            WebSocketService, AuthProvider>(
          create: (context) => AuthProvider(
            api: context.read<ApiService>(),
            crypto: context.read<CryptoService>(),
            storage: context.read<StorageService>(),
            ws: context.read<WebSocketService>(),
          ),
          update: (_, api, crypto, storage, ws, previous) =>
              previous ??
              AuthProvider(
                api: api,
                crypto: crypto,
                storage: storage,
                ws: ws,
              ),
        ),
      ],
      child: MaterialApp(
        title: 'E2E Chat',
        debugShowCheckedModeBanner: false,
        theme: ThemeData(
          colorScheme: ColorScheme.fromSeed(
            seedColor: const Color(0xFF4ECCA3),
            brightness: Brightness.light,
          ),
          useMaterial3: true,
          appBarTheme: const AppBarTheme(
            centerTitle: false,
            elevation: 0,
          ),
          elevatedButtonTheme: ElevatedButtonThemeData(
            style: ElevatedButton.styleFrom(
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(8),
              ),
            ),
          ),
        ),
        darkTheme: ThemeData(
          colorScheme: ColorScheme.fromSeed(
            seedColor: const Color(0xFF4ECCA3),
            brightness: Brightness.dark,
          ),
          useMaterial3: true,
          appBarTheme: const AppBarTheme(
            centerTitle: false,
            elevation: 0,
          ),
        ),
        themeMode: ThemeMode.system,
        home: const AppRoot(),
      ),
    );
  }
}

class AppRoot extends StatefulWidget {
  const AppRoot({super.key});

  @override
  State<AppRoot> createState() => _AppRootState();
}

class _AppRootState extends State<AppRoot> {
  bool _initialized = false;

  @override
  void initState() {
    super.initState();
    _initApp();
  }

  Future<void> _initApp() async {
    // Initialize auth provider
    await context.read<AuthProvider>().init();
    setState(() {
      _initialized = true;
    });
  }

  @override
  Widget build(BuildContext context) {
    if (!_initialized) {
      return const Scaffold(
        body: Center(
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(Icons.lock, size: 64, color: Color(0xFF4ECCA3)),
              SizedBox(height: 16),
              CircularProgressIndicator(),
              SizedBox(height: 16),
              Text('Loading...'),
            ],
          ),
        ),
      );
    }

    return Consumer<AuthProvider>(
      builder: (context, auth, _) {
        switch (auth.state) {
          case AuthState.initial:
          case AuthState.loading:
            return const Scaffold(
              body: Center(child: CircularProgressIndicator()),
            );

          case AuthState.authenticated:
            // Provide ChatProvider when authenticated
            return ChangeNotifierProvider(
              create: (context) => ChatProvider(
                api: context.read<ApiService>(),
                crypto: context.read<CryptoService>(),
                ws: context.read<WebSocketService>(),
                currentUserId: auth.userId!,
              ),
              child: const _AuthenticatedApp(),
            );

          case AuthState.unauthenticated:
          case AuthState.error:
            return const LoginScreen();
        }
      },
    );
  }
}

/// Authenticated app with its own Navigator to keep ChatProvider in scope
class _AuthenticatedApp extends StatefulWidget {
  const _AuthenticatedApp();

  @override
  State<_AuthenticatedApp> createState() => _AuthenticatedAppState();
}

class _AuthenticatedAppState extends State<_AuthenticatedApp> {
  final _navigatorKey = GlobalKey<NavigatorState>();

  @override
  void initState() {
    super.initState();
    // Load chats when authenticated
    WidgetsBinding.instance.addPostFrameCallback((_) {
      context.read<ChatProvider>().loadChats();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Navigator(
      key: _navigatorKey,
      initialRoute: '/',
      onGenerateRoute: (settings) {
        Widget page;
        switch (settings.name) {
          case '/':
            page = const ChatListScreen();
            break;
          case '/chat':
            page = const ChatScreen();
            break;
          case '/new-chat':
            page = const NewChatScreen();
            break;
          case '/settings':
            page = const SettingsScreen();
            break;
          default:
            page = const ChatListScreen();
        }
        return MaterialPageRoute(builder: (_) => page, settings: settings);
      },
    );
  }
}
