## 部署流程 — DataPipe 数据管道

DataPipe 项目的部署流程记录。该项目使用阿里云 ECS 进行服务器部署。

**部署步骤：**
1. 在本地运行 `make build` 编译 Go 二进制文件
2. 使用 `scp` 将二进制文件上传到 ECS 实例
3. 通过 `systemctl restart datapipe` 重启服务
4. 检查 `/var/log/datapipe/app.log` 确认服务正常启动

**环境变量：** 在 `/etc/datapipe/config.env` 中配置数据库连接、Redis 地址和 API 密钥。

**回滚：** 保留最近 5 个版本的二进制文件在 `/opt/datapipe/releases/` 目录下。回滚时软链接切换到旧版本并重启。

**注意事项：** 部署前务必检查数据库迁移状态，运行 `datapipe migrate status` 确认无待执行迁移。周五下午不要部署。

上次部署：2026年3月15日，版本 v2.4.1。
