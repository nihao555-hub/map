# store/

智能引擎本地商户库写在这里：`merchants.db`（git 忽略）。

```bash
export ENGINE_MERCHANT_DB="$PWD/store/merchants.db"
make ingest-sea
```

路径约定和换环境恢复见 [docs/engine-data.md](../docs/engine-data.md)。
