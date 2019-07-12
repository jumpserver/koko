module github.com/jumpserver/koko

go 1.12

require (
	github.com/Azure/azure-pipeline-go v0.1.9
	github.com/Azure/azure-storage-blob-go v0.6.0
	github.com/LeeEirc/elfinder v0.0.0-20190604073433-f4f8357e9220
	github.com/aliyun/aliyun-oss-go-sdk v1.9.8
	github.com/anmitsu/go-shlex v0.0.0-20161002113705-648efa622239
	github.com/aws/aws-sdk-go v1.19.46
	github.com/elastic/go-elasticsearch v0.0.0
	github.com/gliderlabs/ssh v0.2.3-0.20190711180243-866d0ddf7991
	github.com/go-playground/form v3.1.4+incompatible
	github.com/googollee/go-socket.io v1.4.2-0.20190317095603-ed07a7212e28
	github.com/gorilla/mux v1.7.2
	github.com/gorilla/websocket v1.4.0
	github.com/jarcoal/httpmock v1.0.4
	github.com/jmespath/go-jmespath v0.0.0-20180206201540-c2b33e8439af
	github.com/konsorten/go-windows-terminal-sequences v1.0.2
	github.com/kr/fs v0.1.0
	github.com/kr/pty v1.1.4
	github.com/leonelquinteros/gotext v1.4.0
	github.com/mattn/go-runewidth v0.0.4
	github.com/olekukonko/tablewriter v0.0.1
	github.com/pkg/errors v0.8.1
	github.com/pkg/sftp v1.10.0
	github.com/satori/go.uuid v1.2.0
	github.com/sirupsen/logrus v1.4.2
	github.com/xlab/treeprint v0.0.0-20181112141820-a009c3971eca
	golang.org/x/crypto v0.0.0-20190701094942-4def268fd1a4
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0-20170531160350-a96e63847dc3
	gopkg.in/yaml.v2 v2.2.2
)

replace (
	github.com/gliderlabs/ssh v0.2.3-0.20190711180243-866d0ddf7991 => github.com/ibuler/ssh v0.1.6-0.20190509065047-1c00c8e8b607
	github.com/googollee/go-engine.io v1.4.1 => github.com/ibuler/go-engine.io v1.4.2-0.20190529094538-7786d3a289b9
	github.com/googollee/go-socket.io v1.4.2-0.20190317095603-ed07a7212e28 => github.com/LeeEirc/go-socket.io v1.4.2-0.20190610105739-e344e8b5a55a
	golang.org/x/crypto v0.0.0-20190701094942-4def268fd1a4 => github.com/ibuler/crypto v0.0.0-20190509101200-a7099eef26a7
)
