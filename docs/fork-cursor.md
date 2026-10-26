# Cursor — fork additions

## en

**Cursor**

- Experimental quota monitoring for the current Cursor desktop login: opt in from the Cursor tab, Display settings or Provider Accounts → Cursor. Like the official usage page, the panel shows Cursor models and other models separately, with this cycle's spend, an estimated cap (spend ÷ used percentage, marked as an estimate), reset times, additional spend and credit balances; quota meters also support the menu bar and floating window. Requests follow the application proxy.
- Reads only the necessary login keys from the desktop SQLite store; no account switching, token refresh, conversation scanning or credential copies. macOS encrypted login is supported after explicit authorization: select “Authorize encrypted login” on the panel or account page. Only the derived key is cached in memory, cleared when monitoring is disabled or the app exits; choose Allow once or Permanent authorization. Permanent authorization saves only consent and allows one automatic Keychain attempt after restart or re-enabling monitoring; failure requires a manual retry. Turn it off in Cursor settings to cancel reads and clear the memory key. Avoiding future system prompts also requires Always Allow in the system dialog (which authorizes the shared security tool, callable by other processes under your user); the app does not change that permission. Windows/Linux encryption, unknown formats, memory-only and isolated-deployment logins remain unsupported. The internal API can change; a Team account has been verified against the official usage page; other plans and three-platform installation acceptance remain pending. Cursor is not yet available in widgets or CLI management.

**Display and CLI tools**

Local statistics and management below apply to Codex, Claude and Grok. Cursor currently provides quota monitoring only.

- Always-visible `Codex | Claude | Grok | Cursor` tabs. On macOS, Settings › Menu Bar Item previews five styles: quota bars, countdown, battery (horizontal or vertical), quota rings (values optionally inside the rings) and progress text. The menu bar follows the panel or shows any of the four providers, each on its own; quota bars, countdowns and batteries can also stack two providers in one item. An existing “Codex + Grok” choice stays that pair.

## zh

**Cursor**

- 实验性监控 Cursor 桌面当前登录：在 Cursor 栏位、显示设置或“服务商账号 → Cursor”中主动启用。面板按官方用量页分开展示 Cursor 模型与其他模型的用量，附本账期已花金额与估算总额（已花 ÷ 已用百分比，标注“估算”），以及重置时间、额外支出与 Credit 余额，额度条也可用于菜单栏和悬浮窗；请求遵循应用代理设置。
- 只读取桌面 SQLite 中必要的登录字段，不切换账号、不刷新 token、不扫描对话、不复制凭据。macOS 加密登录可在面板或账号页点击“授权读取加密登录”后使用；只在内存缓存派生密钥，关闭监控或退出应用即清除，可选择“仅本次授权”或“永久授权”。永久授权只保存选择，重启或重新开启监控后自动尝试读取一次钥匙串，失败后需手动重试；可在 Cursor 设置关闭，取消读取并清除内存密钥。免除后续系统弹窗仍需在系统框选择“始终允许”（授权的是同一用户的其他进程也可调用的系统 security 工具），应用不修改该系统权限。Windows/Linux 加密、未知格式、仅内存与独立部署登录仍不支持。内部接口可能变化，已用 Team 账号对照官方用量页验证，其他套餐与三端安装验收仍待完成；Cursor 暂不接入 Widget 或 CLI 管理。

**显示与 CLI 管理**

以下本地统计与管理功能适用于 Codex、Claude 和 Grok；Cursor 当前仅提供额度监控。

- 常驻 `Codex | Claude | Grok | Cursor` 标签切换。macOS 设置中的“状态栏组件”可预览额度条、倒计时、电池（横向或竖向）、圆环进度（数值可置于环内）、进度文本五种样式；菜单栏可跟随主面板，或分别显示四个供应商中的任意几个，额度条、倒计时与电池还可将两个供应商上下叠放在同一组件中。已经保存的 “Codex + Grok” 仍是这一对

## zh-hant

**Cursor**

- 實驗性監控 Cursor 桌面目前登入：在 Cursor 頁籤、顯示設定或「服務商帳號 → Cursor」中主動啟用。面板依官方用量頁分開顯示 Cursor 模型與其他模型的用量，附本帳期已花金額與估算總額（已花 ÷ 已用百分比，標註「估算」），以及重置時間、額外支出與 Credit 餘額，額度條也可用於選單列和懸浮視窗；請求遵循應用程式代理設定。
- 唯讀桌面 SQLite 中必要的登入欄位，不切換帳號、不更新 token、不掃描對話、不複製憑證。macOS 加密登入可在面板或帳號頁點擊「授權讀取加密登入」後使用；衍生金鑰僅快取於記憶體，關閉監控或退出即清除，可選擇「僅本次授權」或「永久授權」。永久授權只儲存選擇，重新啟動或開啟監控後自動嘗試讀取一次鑰匙圈，失敗後須手動重試；可在 Cursor 設定關閉，取消讀取並清除記憶體金鑰。免除後續系統提示仍須在系統框選擇「永遠允許」（授權的是同一使用者的其他程序也可呼叫的系統 security 工具），應用程式不修改該系統權限。Windows/Linux 加密、未知格式、僅記憶體與獨立部署登入仍不支援。內部介面可能變更，已用 Team 帳號對照官方用量頁驗證，其他方案與三平台安裝驗收仍待完成；Cursor 暫不接入 Widget 或 CLI 管理。

**顯示與 CLI 管理**

以下本機統計與管理功能適用於 Codex、Claude 和 Grok；Cursor 目前僅提供額度監控。

- 常駐的 `Codex | Claude | Grok | Cursor` 頁籤。macOS 設定中的「狀態欄元件」可預覽額度條、倒計時、電池（橫向或直向）、圓環進度（數值可置於環內）、進度文本五種樣式；選單列可跟隨主面板，或分別顯示四個供應商中的任意幾個，額度條、倒計時與電池還可將兩個供應商上下疊放在同一元件中。已經儲存的「Codex + Grok」仍是這一對。

## ja

**Cursor**

- 現在の Cursor デスクトップログインを対象とした実験的な使用量監視です。Cursor タブ、表示設定、または「プロバイダーアカウント → Cursor」で有効にすると、公式の使用量ページと同じく Cursor モデルとその他のモデルを分けて表示し、今期の支出額と推定上限（支出 ÷ 使用率、「推定」と表示）、リセット時刻、追加支出、クレジット残高を表示します。使用量バーはメニューバーとフローティングウィンドウにも対応し、リクエストはアプリのプロキシ設定に従います。
- デスクトップの SQLite から必要なログインキーのみを読み取ります。アカウント切り替え、トークン更新、会話のスキャン、認証情報のコピーは行いません。macOS の暗号化ログインは、パネルまたはアカウント画面の読み取り許可ボタンから利用できます。派生鍵はメモリ内だけに保持し、監視の無効化や終了時に消去します。今回のみの許可と永続的な許可を選べます。永続的な許可は選択だけを保存し、再起動や監視の再開後に一度自動でキーチェーンを読み取ります。失敗後は手動で再試行します。Cursor 設定でオフにすると読み取りを中止し、メモリ内の鍵を消去します。システム確認を省くにはシステム画面でも「常に許可」が必要です。同じユーザーの他のプロセスも呼び出せる security ツールへの許可であり、アプリはその権限を変更しません。Windows/Linux の暗号化、未知の形式、メモリ内のみ、個別デプロイは未対応です。内部 API は変更される可能性があり、Team アカウントで公式の使用量ページと照合済みですが、その他のプランと 3 プラットフォームのインストール検証は未完了です。Cursor の Widget と CLI 管理は未対応です。

**表示と CLI 管理**

以下のローカル統計と管理機能は Codex、Claude、Grok が対象です。Cursor は現在、使用量の監視のみ提供します。

- 常時表示の `Codex | Claude | Grok | Cursor` タブがあります。macOS の設定「メニューバー項目」では、クォータバー、カウントダウン、バッテリー（横または縦）、クォータリング（値をリング内に表示可能）、進捗テキストの 5 つのスタイルをプレビューできます。メニューバーはメインパネルに従うか、4 つのプロバイダーのうち任意のものを個別に表示し、クォータバー・カウントダウン・バッテリーでは 2 つのプロバイダーを 1 項目に上下に重ねることもできます。保存済みの「Codex + Grok」はその組のままです。

## ko

**Cursor**

- 현재 Cursor 데스크톱 로그인의 실험적 사용량 모니터링입니다. Cursor 탭, 표시 설정 또는 "공급자 계정 → Cursor"에서 켜면 공식 사용량 페이지처럼 Cursor 모델과 기타 모델을 나누어 표시하고, 이번 주기 지출액과 추정 한도(지출 ÷ 사용률, "추정"으로 표시), 재설정 시각, 추가 지출과 크레딧 잔액을 보여 줍니다. 사용량 막대는 메뉴 막대와 플로팅 창에서도 사용할 수 있으며 요청은 앱의 프록시 설정을 따릅니다.
- 데스크톱 SQLite에서 필요한 로그인 키만 읽습니다. 계정 전환, 토큰 갱신, 대화 스캔이나 자격 증명 복사는 하지 않습니다. macOS 암호화 로그인은 패널 또는 계정 페이지의 읽기 허용 버튼으로 사용할 수 있습니다. 파생 키는 메모리에만 보관하고 모니터링을 끄거나 종료하면 지웁니다. 이번만 허용 또는 영구 허용을 선택할 수 있습니다. 영구 허용은 선택만 저장하고 재시작이나 모니터링 재활성화 후 키체인 읽기를 한 번 자동 시도합니다. 실패 후에는 수동 재시도가 필요합니다. Cursor 설정에서 끄면 읽기를 취소하고 메모리 키를 지웁니다. 이후 시스템 창을 없애려면 시스템 창에서도 항상 허용을 선택해야 합니다. 이는 같은 사용자의 다른 프로세스도 호출할 수 있는 security 도구에 대한 권한이며 앱은 해당 시스템 권한을 변경하지 않습니다. Windows/Linux 암호화, 알 수 없는 형식, 메모리 전용 및 별도 배포 로그인은 지원하지 않습니다. 내부 API는 변경될 수 있으며 Team 계정으로 공식 사용량 페이지와 대조해 확인했으며, 다른 요금제와 세 플랫폼 설치 검증은 아직 필요합니다. Cursor 위젯과 CLI 관리는 제공하지 않습니다.

**표시 및 CLI 관리**

아래 로컬 통계와 관리 기능은 Codex, Claude, Grok에 적용됩니다. Cursor는 현재 사용량 모니터링만 제공합니다.

- 항상 보이는 `Codex | Claude | Grok | Cursor` 탭. macOS 설정의 “메뉴 막대 항목”에서 할당량 막대, 카운트다운, 배터리(가로 또는 세로), 할당량 링(값을 링 안에 표시 가능), 진행 텍스트 다섯 가지 스타일을 미리 볼 수 있습니다. 메뉴 막대는 주 패널을 따르거나 네 공급자 중 원하는 공급자를 각각 따로 표시하며, 할당량 막대·카운트다운·배터리는 두 공급자를 한 항목에 위아래로 쌓을 수도 있습니다. 이미 저장된 “Codex + Grok”은 그 쌍을 유지합니다.

## fr

**Cursor**

- Suivi expérimental du quota de la connexion actuelle à Cursor : à activer dans l’onglet Cursor, les réglages d’affichage ou « Comptes fournisseurs → Cursor ». Comme la page d’utilisation officielle, le panneau distingue les modèles Cursor et les autres modèles, avec les dépenses du cycle et un plafond estimé (dépenses ÷ pourcentage utilisé, signalé comme estimation), ainsi que les réinitialisations, les dépenses supplémentaires et les soldes de crédits. Les jauges sont aussi disponibles dans la barre de menus et la fenêtre flottante. Les requêtes suivent le proxy de l’application.
- Seules les clés de connexion nécessaires sont lues dans SQLite, sans changer de compte, renouveler de jeton, lire les conversations ni copier les identifiants. La connexion chiffrée macOS est accessible via le bouton d’autorisation du panneau ou de la page du compte. Seule la clé dérivée reste en mémoire ; elle est effacée à l’arrêt du suivi ou de l’application. Vous pouvez autoriser une fois ou choisir l’autorisation permanente. Celle-ci ne mémorise que le consentement et permet une tentative automatique de lecture du trousseau après redémarrage ou réactivation du suivi ; après un échec, réessayez manuellement. La désactiver dans les réglages Cursor annule la lecture et efface la clé en mémoire. Éviter les demandes système requiert aussi Toujours autoriser dans la boîte système, ce qui autorise l’outil security également accessible aux autres processus du même utilisateur. L’application ne modifie pas cette permission. Le chiffrement Windows/Linux, les formats inconnus, les connexions uniquement en mémoire et les déploiements distincts restent non pris en charge. L’API interne peut changer ; un compte Team a été vérifié avec la page d’utilisation officielle ; les autres offres et l’installation sur les trois plateformes restent à valider. Cursor ne propose pas encore de widgets ni de gestion CLI.

**Affichage et outils CLI**

Les statistiques locales et les fonctions de gestion ci-dessous concernent Codex, Claude et Grok. Cursor propose uniquement le suivi des quotas pour le moment.

- Onglets `Codex | Claude | Grok | Cursor` toujours visibles. Sur macOS, Réglages › Élément de la barre de menus présente un aperçu de cinq styles : barres de quota, compte à rebours, batterie (horizontale ou verticale), anneaux de quota (valeurs éventuellement à l’intérieur) et texte de progression. La barre de menus suit le panneau ou affiche n’importe lesquels des quatre fournisseurs, chacun à part ; les barres, comptes à rebours et batteries peuvent aussi empiler deux fournisseurs dans un même élément. Un widget ou un choix déjà enregistré « Codex + Grok » conserve cette paire.

## ru

**Cursor**

- Экспериментальный мониторинг текущего входа в настольный Cursor: включается на вкладке Cursor, в настройках отображения или в разделе «Аккаунты провайдеров → Cursor». Как и официальная страница использования, панель раздельно показывает модели Cursor и другие модели с расходами за текущий период и оценкой лимита (расход ÷ процент использования, помечено как оценка), а также время сброса, дополнительные расходы и остатки кредитов. Индикаторы квоты также доступны в строке меню и плавающем окне. Запросы используют прокси приложения.
- Читаются только необходимые ключи входа из SQLite. Нет переключения аккаунтов, обновления токенов, чтения диалогов или копирования учётных данных. Зашифрованный вход macOS доступен через кнопку разрешения чтения на панели или странице аккаунта. Только производный ключ хранится в памяти и удаляется при отключении мониторинга или выходе; можно выбрать разовое или постоянное разрешение. Постоянное сохраняет только согласие и разрешает одну автоматическую попытку чтения Связки ключей после перезапуска или включения мониторинга; после ошибки повторите вручную. Отключение в настройках Cursor отменяет чтение и удаляет ключ из памяти. Для отсутствия системных запросов нужно также выбрать «Разрешать всегда» в системном окне. Это разрешение утилите security, доступной и другим процессам вашего пользователя; приложение его не меняет. Шифрование Windows/Linux, неизвестные форматы, вход только в памяти и отдельные развёртывания пока не поддерживаются. Внутренний API может меняться; аккаунт Team сверен с официальной страницей использования; другие тарифы и установка на трёх платформах ещё не проверены. Виджеты и управление CLI для Cursor пока недоступны.

**Отображение и инструменты CLI**

Локальная статистика и управление ниже относятся к Codex, Claude и Grok. Cursor пока предоставляет только мониторинг квоты.

- Всегда видимые вкладки `Codex | Claude | Grok | Cursor`. На macOS в настройках «Значок в строке меню» можно посмотреть пять стилей: полосы квоты, обратный отсчёт, батарея (горизонтально или вертикально), кольца квоты (значения можно показывать внутри колец) и текст прогресса. Строка меню следует панели или показывает любых из четырёх провайдеров, каждого отдельно; полосы, отсчёт и батарея могут также ставить двух провайдеров друг над другом в одном элементе. Уже сохранённая пара «Codex + Grok» остаётся этой парой.
