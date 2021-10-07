# aws-cost-notify-to-slack

AWSの利用料金をSlackに通知するツールのサンプルです。

## 環境変数
実行前に事前に準備が必要です。
```
SLACK_ENDPOINT: Webhookの投稿先URLです
SLACK_API_TOKEN: files.uploadで用いるToken文字列です
SLACK_CHANNEL: Slackの投稿先チャンネルです
AWS_ACCOUNT: Slackに投稿される箇条書き文字列の冒頭にある`AWS Account:`に付与する値です
```