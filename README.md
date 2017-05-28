# slack-approver

Sorry! Only Japanese document is available.

slackのinteractive messageで簡易な承認プロセスを実現するアプリです。

CIに組み込んだり、認証のMFAに組み込んだりすると便利かもしれません。


## 導入

### URLの準備
アクセスを受け付けるhttp(s)なURLが必要になります。

### slack側

1. https://api.slack.com/apps でアプリを作成
2. interactive-messagesを有効化
  - Request URLに"https://YOUR-DOMAIN/interactive_action_callback"を設定
3. Permission設定
  - "chat:write:bot"権限が必要
4. アプリインストール

Verification TokenとOAuth Access Tokenがアプリを動かすのに必要になります。

### アプリ起動

```
docker build -t slack-approver .
docker run -d -p 8080:8080 \
  -e API_TOKEN=... \
  -e VERIFICATION_TOKEN=...
  slack-approver
```

環境変数は以下を参考にしてください。

- API_TOKEN : slackのアプリ設定のOAuth Access Tokenを指定(必須)
- VERIFICATION_TOKEN : slackのアプリ設定のVerification Tokenを指定(必須)
- PORT: http : リクエストを受け付けるポート(デフォルト: 8080)
- REQUEST_PATH : リクエスト受け付け用(デフォルト: /ask)
- USER_NAME : slack投稿時のユーザ名
- ICON_EMOJI : slack投稿時のアイコン用絵文字


### 使い方

POSTでアクセスするとアプリがslack側にメッセージが投稿します。  
そこで承認されると200 OKなレスポンスが返ってくるので、処理を分岐させることが可能です。


```
if curl -sf -XPOST https://YOUR-DOMAIN/ask -d "ch=#your_channel" -d msg="hogehoge"; then
  echo "approved!"
else
  echo "not approved."
fi
```

承認されるか、タイムアウト(デフォルト60秒)するまでレスポンスが帰ってこないので、クライアントのタイムアウトは長めにしておいてください。

なお、タイムアウトはリクエスト時にtimeout=300などのように明示的に指定することもできます(最大600秒)
