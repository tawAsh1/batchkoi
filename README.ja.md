# batchkoi 🎣

バッチこい！ AWS Batch のジョブ定義をコードで管理する小さなデプロイツールです。
ECS における [ecspresso](https://github.com/kayac/ecspresso)、Lambda における
[lambroll](https://github.com/fujiwara/lambroll) の Batch 版を目指しています。

[English README](README.md)

Batch のジョブ定義はリビジョン管理されていて、イメージタグを更新するたびに新しい
リビジョンが積み上がります。この回転の速い部分を Terraform で追いかけるのはつらいので、
そこだけを batchkoi が引き受けます。管理対象はジョブ定義のみで、コンピューティング環境や
ジョブキューは Terraform/CDK の担当のままです。

## インストール

```sh
go install github.com/tawAsh1/batchkoi@latest
```

[Releases](https://github.com/tawAsh1/batchkoi/releases) にビルド済みバイナリもあります。

GitHub Actions では同梱の setup action でインストールできます。デフォルトで Sigstore の
ビルド来歴（attestation）を検証します。タグは付け替え可能で、アセットの隣にある checksum は
攻撃者も差し替えられるため、信頼の根拠になるのは attestation だけです（2026 年の trivy の
インシデントが実例）。

```yaml
- uses: tawAsh1/batchkoi@<commit-sha>   # action はタグでなく commit SHA で固定
  with:
    version: v0.3.0                     # バイナリのバージョンも固定推奨
- run: batchkoi deploy --ext-str IMAGE_TAG=${{ github.sha }}
```

## 使い方

```sh
batchkoi init --jd my-jobdef     # AWS 上の定義から batchkoi.yml + jobdef.json を生成
batchkoi diff                    # ローカル定義と最新リビジョンの差分
batchkoi verify                  # キュー・IAM ロール・イメージ・シークレット・ロググループの存在確認
batchkoi deploy --keep-count 5   # 変更があれば登録し、新しい 5 リビジョンだけ残す
batchkoi run -q my-queue         # ジョブを投入して CloudWatch Logs を tail
```

カレントディレクトリの `batchkoi.yml` を読みます（`-c` で変更可）。

## 設定

ecspresso と同じく、ツール設定とジョブ定義の 2 ファイル構成です。

```yaml
# batchkoi.yml
region: ap-northeast-1
job_definition: jobdef.jsonnet
# required_version: ">= 0.1.0"
# job_queue: my-job-queue        # run のデフォルトキュー
plugins:
  - name: tfstate
    config: { path: terraform.tfstate }
```

`batchkoi.yml` 自体も Go テンプレートとして描画されます（ecspresso 互換）。
`{{ env "NAME" "default" }}` / `{{ must_env "NAME" }}` が使えるので、たとえば
`job_queue: '{{ env "JOB_QUEUE" "default-q" }}'` のように環境変数から値を渡せます。

```jsonnet
// jobdef.jsonnet — RegisterJobDefinition のリクエストをそのまま書く
local env = std.native('env');
local tfstate = std.native('tfstate');
{
  jobDefinitionName: 'myapp',
  type: 'container',
  platformCapabilities: ['FARGATE'],
  containerProperties: {
    image: 'myapp:' + env('IMAGE_TAG', 'latest'),
    executionRoleArn: tfstate('aws_iam_role.batch_exec.arn'),
    resourceRequirements: [
      { type: 'VCPU', value: '0.25' },
      { type: 'MEMORY', value: '512' },
    ],
  },
}
```

ネイティブ関数は `env(name, default)` / `must_env(name)` / `caller_identity()` /
`ecr_digest(image)`（常時）と、プラグインで有効になる `tfstate(addr)` / `ssm(name)`。
`ecr_digest` はプライベート ECR のイメージ URI（`:tag` 省略時は `latest`）を `sha256:...`
ダイジェストに解決します。人間はタグを書きつつ、デプロイはダイジェストで固定できます:

```jsonnet
local repo = '123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/myapp';
{ containerProperties: { image: repo + '@' + std.native('ecr_digest')(repo + ':' + env('IMAGE_TAG', 'latest')) } }
```

`--ext-str KEY=VALUE` / `--ext-code` で
`std.extVar` に値を渡せます。`--envfile .env` で環境変数ファイルを読み込み、すべてのフラグは
`BATCHKOI_*` 環境変数でも指定できます。

`.json` のジョブ定義はそのまま読み込まれます — Jsonnet 評価を通らないため、ネイティブ関数や
`--ext-str` / `--ext-code` は効きません。必要なら `.jsonnet` を使ってください（JSON は
そのまま valid な Jsonnet なので、リネームするだけで動きます）。

動かせるサンプルは [_example/](_example/) にあります（render だけなら AWS アカウント不要）。

## コマンド

| コマンド | 動作 |
|---|---|
| `init` | 既存のジョブ定義から設定ファイル一式を生成（`--jd name[:rev]`、`--jsonnet`） |
| `render` | 定義を評価して JSON を出力 |
| `diff` | 登録済みリビジョンとの差分（`--rev N` で固定、`--exit-code` は差分ありで exit 2） |
| `verify` | 参照先リソースの存在確認。NG があれば非ゼロ終了（`--queue` は run と同じ） |
| `register` | 無条件に新リビジョンを登録（`--dry-run` で内容と番号を事前表示） |
| `deploy` | 変更時のみ登録し、古いリビジョンを整理（`--keep-count` / `--keep-revision` / `--dry-run`） |
| `revisions` | リビジョン一覧。ステータス・イメージ・タグ・latest 表示（`--active`） |
| `rollback` | 最新 ACTIVE リビジョンを deregister して一つ前を latest に戻す（`--dry-run`） |
| `deregister` | 登録せずにリビジョン整理だけ行う |
| `run` | ジョブ投入とログ tail。変更時のみ事前登録（`--rev` / `--command` / `--env` / `--array N` / `--no-wait` / `--dry-run`） |
| `logs` | 既存ジョブのログを job id で表示（array の子は `<job-id>:<index>`、`--follow` で追跡） |
| `list` | リージョン内のジョブ定義を 1 行ずつ一覧（`--all`、設定ファイルなしでも動作） |

`--keep-count` を渡さない限り deregister は一切起きません（`--keep-revision` は
その整理から特定リビジョンを守るためのものです）。削除対象は `deploy --dry-run` で
事前に確認できます。`run` はジョブ失敗時に非ゼロで終了し、どのコマンドも `-o json` で
機械可読な出力になります。`run --array N` は array ジョブとして投入し、子ジョブのログを
docker-compose 風の色付き prefix で interleave 表示、進捗バーで完了状況も追えます。
32 子を超える array は 32 子ずつのページに分かれ、←/→（または p/n）でページを
切り替えられます（CloudWatch の API クォータ対策。非対話実行では進捗表示のみ）。
マルチノードジョブは投入はできますがログは追いません。

注意点をいくつか:

- rollback は「最新 ACTIVE の deregister」なので、ACTIVE リビジョンが 2 つ以上必要です。
  `--keep-count` は 2 以上を推奨します（`--keep-count 1` には警告が出ます）
- `--command` / `--env` は SubmitJob の containerOverrides を使うため、ECS/Fargate の
  コンテナジョブにしか効きません。EKS・マルチノード定義では無視されます（警告が出ます）
- 非推奨の `containerProperties.vcpus` / `memory` は使わないでください。AWS がサーバー側で
  `resourceRequirements` に書き換えるため diff が永遠に差分ありになり、deploy のたびに
  新リビジョンが登録されてしまいます（検出時に警告が出ます）

## 設計

- 管理するのはジョブ定義だけ。毎デプロイで変わる部分に絞る
- 設定ファイルは API のリクエスト形そのもの。独自スキーマを覚える必要はない
- 収束待ちはない。Batch のジョブは使い捨てなので、deploy は新リビジョンの登録、
  rollback はその取り消しがすべて

## 謝辞

[fujiwara](https://github.com/fujiwara) さんの
[lambroll](https://github.com/fujiwara/lambroll) と
[ecspresso](https://github.com/kayac/ecspresso) に直接影響を受けています。
設定モデルも Jsonnet ネイティブ関数も CLI の操作感もこれらに倣いました。
[tfstate-lookup](https://github.com/fujiwara/tfstate-lookup) を利用しています。

## ライセンス

MIT
