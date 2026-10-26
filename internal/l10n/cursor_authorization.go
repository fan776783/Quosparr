package l10n

func (l Localizer) CursorAuthorization(key string) string {
	v, ok := cursorAuthorizationStrings[key]
	if !ok {
		return key
	}
	for i, language := range Languages() {
		if language == l.Language() {
			return v[i]
		}
	}
	return v[0]
}

var cursorAuthorizationStrings = map[string][7]string{
	"action":    {"Authorize encrypted login", "授权读取加密登录", "授權讀取加密登入", "암호화 로그인 읽기 허용", "暗号化ログインの読み取りを許可", "Разрешить чтение входа", "Autoriser la lecture chiffrée"},
	"title":     {"Cursor login authorization", "Cursor 登录读取授权", "Cursor 登入讀取授權", "Cursor 로그인 읽기 권한", "Cursor ログインの読み取り許可", "Доступ к входу Cursor", "Autorisation de lecture Cursor"},
	"message":   {"Read the current Cursor login's decryption key from macOS Keychain to display usage?", "是否从 macOS 钥匙串读取当前 Cursor 登录的解密密钥，以显示额度？", "是否從 macOS 鑰匙圈讀取目前 Cursor 登入的解密金鑰，以顯示額度？", "사용량을 표시하기 위해 macOS 키체인에서 현재 Cursor 로그인 복호화 키를 읽을까요?", "使用量を表示するため、macOS キーチェーンから現在の Cursor ログインの復号鍵を読み取りますか？", "Прочитать ключ расшифровки текущего входа Cursor из Связки ключей macOS для отображения квоты?", "Lire la clé de déchiffrement de la connexion Cursor actuelle dans le trousseau macOS pour afficher les quotas ?"},
	"once":      {"Allow once", "仅本次授权", "僅本次授權", "이번만 허용", "今回のみ許可", "Разрешить один раз", "Autoriser une fois"},
	"permanent": {"Permanent authorization", "永久授权", "永久授權", "영구 허용", "永続的な許可", "Постоянное разрешение", "Autorisation permanente"},
	"rememberHint": {
		"Remember consent and read the key again after restart. Turn off to cancel reads and clear the in-memory key; system Keychain permissions stay unchanged.",
		"记住授权选择，重启后自动读取密钥。关闭可取消读取并清除内存密钥；不会修改系统钥匙串权限。",
		"記住授權選擇，重新啟動後自動讀取金鑰。關閉可取消讀取並清除記憶體金鑰；不會修改系統鑰匙圈權限。",
		"허용 선택을 기억하고 재시작 후 키를 다시 읽습니다. 끄면 읽기를 취소하고 메모리 키를 지웁니다. 시스템 키체인 권한은 유지됩니다.",
		"許可を記憶し、再起動後に鍵を自動で読み取ります。オフにすると読み取りを中止し、メモリ内の鍵を消去します。システムのキーチェーン権限は変更しません。",
		"Запоминать согласие и читать ключ после перезапуска. Отключение отменяет чтение и удаляет ключ из памяти, не меняя системных разрешений Связки ключей.",
		"Mémorise le consentement et relit la clé après redémarrage. Désactiver annule la lecture et efface la clé en mémoire, sans modifier les permissions système du trousseau.",
	},
	"detail": {
		"Allow once keeps the derived key only until monitoring stops or the app quits. Permanent authorization saves only your choice and permits one automatic Keychain attempt after restart or re-enabling monitoring; failure requires a manual retry. Cancel it in Cursor settings. Keys and tokens are never saved. The system prompt names security and Cursor Safe Storage. To avoid future system prompts, choose Always Allow there. That grants access to the system security tool, which other processes under your user can also invoke. Quosparr does not change system permissions.",
		"“仅本次授权”在关闭监控或退出应用后失效。“永久授权”只保存你的选择，重启或重新开启监控后自动尝试读取一次钥匙串；失败后需手动重试，可在 Cursor 设置中取消。密钥与 token 不落盘。系统授权框显示 security 和 Cursor Safe Storage；如需免除后续系统弹窗，还需在该框选择“始终允许”。这会授权系统 security 工具，同一用户的其他进程也能调用它访问该条目。Quosparr 不修改系统权限。",
		"「僅本次授權」在關閉監控或退出後失效。「永久授權」只儲存你的選擇，重新啟動或開啟監控後自動嘗試讀取一次鑰匙圈；失敗後須手動重試，可在 Cursor 設定中取消。金鑰與 token 不落盤。系統授權框顯示 security 和 Cursor Safe Storage；如需免除後續系統提示，還須選擇「永遠允許」。這會授權系統 security 工具，同一使用者的其他程序也能呼叫它存取此項目。Quosparr 不修改系統權限。",
		"이번만 허용은 모니터링을 끄거나 앱을 종료하면 만료됩니다. 영구 허용은 선택만 저장하고 재시작 또는 모니터링 재활성화 후 키체인 읽기를 한 번 시도합니다. 실패하면 수동으로 재시도하세요. Cursor 설정에서 취소할 수 있습니다. 키와 토큰은 디스크에 저장하지 않습니다. 시스템 창에는 security와 Cursor Safe Storage가 표시됩니다. 이후 시스템 창을 없애려면 항상 허용을 선택하세요. 이는 같은 사용자의 다른 프로세스도 호출할 수 있는 시스템 security 도구에 권한을 줍니다. Quosparr는 시스템 권한을 변경하지 않습니다.",
		"今回のみの許可は監視の無効化やアプリ終了で失効します。永続的な許可は選択だけを保存し、再起動や監視の再開後にキーチェーンを一度自動で読み取ります。失敗後は手動で再試行してください。Cursor 設定で取り消せます。鍵やトークンは保存しません。システム画面には security と Cursor Safe Storage が表示されます。以後のシステム確認を省くには「常に許可」を選んでください。同じユーザーの他のプロセスも呼び出せるシステムの security ツールに権限を与えます。Quosparr はシステム権限を変更しません。",
		"Разовое разрешение действует до отключения мониторинга или выхода. Постоянное сохраняет только ваш выбор и разрешает одну попытку чтения Связки ключей после перезапуска или включения мониторинга. После ошибки повторите вручную. Отменить можно в настройках Cursor. Ключи и токены не сохраняются на диск. В системном окне указаны security и Cursor Safe Storage. Для отсутствия последующих системных запросов выберите «Разрешать всегда». Это даёт доступ утилите security, которую могут вызывать и другие процессы вашего пользователя. Quosparr не меняет системные разрешения.",
		"L’autorisation ponctuelle expire à l’arrêt du suivi ou de l’application. L’autorisation permanente ne conserve que votre choix et permet une tentative automatique de lecture du trousseau après redémarrage ou réactivation du suivi. Après un échec, réessayez manuellement. Révocable dans les réglages Cursor. Les clés et jetons ne sont jamais enregistrés. La demande système indique security et Cursor Safe Storage. Pour éviter les demandes système ultérieures, choisissez Toujours autoriser. Cela donne accès à l’outil système security, que d’autres processus de votre utilisateur peuvent aussi appeler. Quosparr ne modifie pas les permissions système.",
	},
	"required":    {"This Cursor login is encrypted. Authorize reading it to display usage.", "当前 Cursor 登录已加密，请授权读取以显示额度。", "目前 Cursor 登入已加密，請授權讀取以顯示額度。", "Cursor 로그인이 암호화되어 있습니다. 사용량을 보려면 읽기를 허용하세요.", "Cursor ログインは暗号化されています。使用量を表示するには読み取りを許可してください。", "Вход Cursor зашифрован. Разрешите чтение для отображения квоты.", "Cette connexion Cursor est chiffrée. Autorisez sa lecture pour afficher les quotas."},
	"denied":      {"Keychain access was cancelled or denied. Use the authorization button to retry; background refresh will not prompt again.", "钥匙串授权已取消或拒绝。可点击授权按钮重试，后台不会再次弹窗。", "鑰匙圈授權已取消或拒絕。可點擊授權按鈕重試，背景不會再次彈窗。", "키체인 접근이 취소되거나 거부되었습니다. 허용 버튼으로 재시도하세요. 백그라운드에서는 다시 묻지 않습니다.", "キーチェーンへのアクセスがキャンセルまたは拒否されました。許可ボタンで再試行できます。バックグラウンドでは再度表示しません。", "Доступ к Связке ключей отменён или запрещён. Повторите через кнопку разрешения; фоновых запросов не будет.", "Accès au trousseau annulé ou refusé. Réessayez avec le bouton d’autorisation ; aucune nouvelle demande en arrière-plan."},
	"unavailable": {"The Cursor decryption key could not be read or used. Unlock your login Keychain and try authorizing again.", "无法读取或使用 Cursor 解密密钥，请解锁登录钥匙串后重新授权。", "無法讀取或使用 Cursor 解密金鑰，請解鎖登入鑰匙圈後重新授權。", "Cursor 복호화 키를 읽거나 사용할 수 없습니다. 로그인 키체인을 잠금 해제하고 다시 허용하세요.", "Cursor の復号鍵を読み取るか使用することができません。ログインキーチェーンのロックを解除して再度許可してください。", "Не удалось прочитать или использовать ключ Cursor. Разблокируйте Связку ключей входа и повторите разрешение.", "Impossible de lire ou d’utiliser la clé Cursor. Déverrouillez le trousseau de session et réessayez."},
	"unsupported": {"This system or Cursor encryption format is not supported yet.", "当前系统或 Cursor 加密格式暂不支持读取。", "目前系統或 Cursor 加密格式暫不支援讀取。", "이 시스템 또는 Cursor 암호화 형식은 아직 지원되지 않습니다.", "このシステムまたは Cursor の暗号化形式はまだサポートされていません。", "Эта система или формат шифрования Cursor пока не поддерживается.", "Ce système ou format de chiffrement Cursor n’est pas encore pris en charge."},
	"isolated":    {"This Cursor login uses a separate deployment, which is not supported yet.", "当前 Cursor 登录使用独立部署，暂不支持读取。", "目前 Cursor 登入使用獨立部署，暫不支援讀取。", "이 Cursor 로그인은 별도 배포를 사용하여 아직 지원되지 않습니다.", "この Cursor ログインは個別のデプロイを使用しており、未対応です。", "Этот вход Cursor использует отдельное развёртывание, которое пока не поддерживается.", "Cette connexion Cursor utilise un déploiement distinct, non pris en charge pour le moment."},
}
