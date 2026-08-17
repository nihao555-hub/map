# store/

智能引擎本地商户库写在这里：`merchants.db`（git 忽略）。

```bash
export ENGINE_MERCHANT_DB="$PWD/store/merchants.db"
make ingest-sea
```

换环境用 OSS：`make upload-merchants` / `make restore-merchants`（需 `ENGINE_OSS_*`）。路径约定见 [docs/engine-data.md](../docs/engine-data.md)。
