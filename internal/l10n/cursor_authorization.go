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
	"action":  {"Authorize encrypted login", "授权读取加密登录", "授權讀取加密登入", "암호화 로그인 읽기 허용", "暗号化ログインの読み取りを許可", "Разрешить чтение входа", "Autoriser la lecture chiffrée"},
	"title":   {"Cursor login authorization", "Cursor 登录读取授权", "Cursor 登入讀取授權", "Cursor 로그인 읽기 권한", "Cursor ログインの読み取り許可", "Доступ к входу Cursor", "Autorisation de lecture Cursor"},
	"message": {"Read the current Cursor login's decryption key from macOS Keychain to display usage?", "是否从 macOS 钥匙串读取当前 Cursor 登录的解密密钥，以显示额度？", "是否從 macOS 鑰匙圈讀取目前 Cursor 登入的解密金鑰，以顯示額度？", "사용량을 표시하기 위해 macOS 키체인에서 현재 Cursor 로그인 복호화 키를 읽을까요?", "使用量を表示するため、macOS キーチェーンから現在の Cursor ログインの復号鍵を読み取りますか？", "Прочитать ключ расшифровки текущего входа Cursor из Связки ключей macOS для отображения квоты?", "Lire la clé de déchiffrement de la connexion Cursor actuelle dans le trousseau macOS pour afficher les quotas ?"},
	"detail": {
		"The system prompt names security and Cursor Safe Storage. Allow is enough for this run: the derived key stays in memory and is cleared when monitoring is turned off or the app quits. Login data is never changed. Always Allow grants access to the system security tool, which other processes under your user can also invoke. After restarting, use this button again; background refresh never opens a permission prompt.",
		"系统授权框会显示 security 和 Cursor Safe Storage。选择“允许”即可用于本次运行：派生密钥只保存在内存，关闭监控或退出应用时清除，不修改登录数据。“始终允许”会授权系统 security 工具，同一用户下的其他进程也能调用它读取该条目。重启应用后请再次点击此按钮；后台刷新不会弹出授权框。",
		"系統授權框會顯示 security 和 Cursor Safe Storage。選擇「允許」即可用於本次執行：衍生金鑰只保留在記憶體，關閉監控或退出應用程式時清除，不修改登入資料。「永遠允許」會授權系統 security 工具，同一使用者下的其他程序也能呼叫它讀取此項目。重新啟動後請再次點擊此按鈕；背景重新整理不會彈出授權框。",
		"시스템 대화상자에 security와 Cursor Safe Storage가 표시됩니다. 이번 실행에는 허용이면 충분합니다. 파생 키는 메모리에만 보관하며 모니터링을 끄거나 앱을 종료하면 지웁니다. 로그인 데이터는 변경하지 않습니다. 항상 허용은 같은 사용자의 다른 프로세스도 호출할 수 있는 시스템 security 도구에 권한을 줍니다. 앱 재시작 후 이 버튼을 다시 누르세요. 백그라운드 새로 고침은 권한 창을 열지 않습니다.",
		"システムの確認画面には security と Cursor Safe Storage が表示されます。「許可」で今回の実行には十分です。派生鍵はメモリ内だけに保持し、監視を無効にするか終了すると消去します。ログインデータは変更しません。「常に許可」は、同じユーザーの他のプロセスも呼び出せるシステムの security ツールに権限を与えます。再起動後はこのボタンを再度押してください。バックグラウンド更新では許可画面を開きません。",
		"В системном окне указаны security и Cursor Safe Storage. Для текущего запуска достаточно «Разрешить»: производный ключ хранится только в памяти и удаляется при отключении мониторинга или выходе. Данные входа не изменяются. «Разрешать всегда» даёт доступ системной утилите security, которую могут вызывать и другие процессы вашего пользователя. После перезапуска нажмите эту кнопку снова; фоновое обновление не вызывает окно разрешения.",
		"La demande système indique security et Cursor Safe Storage. Autoriser suffit pour cette exécution : la clé dérivée reste en mémoire et est effacée à l’arrêt du suivi ou de l’application. Les données de connexion ne sont jamais modifiées. Toujours autoriser donne accès à l’outil système security, que d’autres processus de votre utilisateur peuvent aussi appeler. Après un redémarrage, utilisez à nouveau ce bouton ; les actualisations en arrière-plan n’ouvrent aucune demande d’autorisation.",
	},
	"required":    {"This Cursor login is encrypted. Authorize reading it to display usage.", "当前 Cursor 登录已加密，请授权读取以显示额度。", "目前 Cursor 登入已加密，請授權讀取以顯示額度。", "Cursor 로그인이 암호화되어 있습니다. 사용량을 보려면 읽기를 허용하세요.", "Cursor ログインは暗号化されています。使用量を表示するには読み取りを許可してください。", "Вход Cursor зашифрован. Разрешите чтение для отображения квоты.", "Cette connexion Cursor est chiffrée. Autorisez sa lecture pour afficher les quotas."},
	"denied":      {"Keychain access was cancelled or denied. Use the authorization button to retry; background refresh will not prompt again.", "钥匙串授权已取消或拒绝。可点击授权按钮重试，后台不会再次弹窗。", "鑰匙圈授權已取消或拒絕。可點擊授權按鈕重試，背景不會再次彈窗。", "키체인 접근이 취소되거나 거부되었습니다. 허용 버튼으로 재시도하세요. 백그라운드에서는 다시 묻지 않습니다.", "キーチェーンへのアクセスがキャンセルまたは拒否されました。許可ボタンで再試行できます。バックグラウンドでは再度表示しません。", "Доступ к Связке ключей отменён или запрещён. Повторите через кнопку разрешения; фоновых запросов не будет.", "Accès au trousseau annulé ou refusé. Réessayez avec le bouton d’autorisation ; aucune nouvelle demande en arrière-plan."},
	"unavailable": {"The Cursor decryption key could not be read or used. Unlock your login Keychain and try authorizing again.", "无法读取或使用 Cursor 解密密钥，请解锁登录钥匙串后重新授权。", "無法讀取或使用 Cursor 解密金鑰，請解鎖登入鑰匙圈後重新授權。", "Cursor 복호화 키를 읽거나 사용할 수 없습니다. 로그인 키체인을 잠금 해제하고 다시 허용하세요.", "Cursor の復号鍵を読み取るか使用することができません。ログインキーチェーンのロックを解除して再度許可してください。", "Не удалось прочитать или использовать ключ Cursor. Разблокируйте Связку ключей входа и повторите разрешение.", "Impossible de lire ou d’utiliser la clé Cursor. Déverrouillez le trousseau de session et réessayez."},
	"unsupported": {"This system or Cursor encryption format is not supported yet.", "当前系统或 Cursor 加密格式暂不支持读取。", "目前系統或 Cursor 加密格式暫不支援讀取。", "이 시스템 또는 Cursor 암호화 형식은 아직 지원되지 않습니다.", "このシステムまたは Cursor の暗号化形式はまだサポートされていません。", "Эта система или формат шифрования Cursor пока не поддерживается.", "Ce système ou format de chiffrement Cursor n’est pas encore pris en charge."},
	"isolated":    {"This Cursor login uses a separate deployment, which is not supported yet.", "当前 Cursor 登录使用独立部署，暂不支持读取。", "目前 Cursor 登入使用獨立部署，暫不支援讀取。", "이 Cursor 로그인은 별도 배포를 사용하여 아직 지원되지 않습니다.", "この Cursor ログインは個別のデプロイを使用しており、未対応です。", "Этот вход Cursor использует отдельное развёртывание, которое пока не поддерживается.", "Cette connexion Cursor utilise un déploiement distinct, non pris en charge pour le moment."},
}
