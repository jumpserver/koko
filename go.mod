module github.com/jumpserver/koko

go 1.12

require (
	github.com/Azure/azure-pipeline-go v0.1.9 // indirect
	github.com/Azure/azure-storage-blob-go v0.6.0
	github.com/BurntSushi/toml v0.3.1 // indirect
	github.com/LeeEirc/elfinder v0.0.11-0.20191224095556-900471613ab8
	github.com/aliyun/aliyun-oss-go-sdk v1.9.8
	github.com/anmitsu/go-shlex v0.0.0-20161002113705-648efa622239 // indirect
	github.com/aws/aws-sdk-go v1.19.46
	github.com/baiyubin/aliyun-sts-go-sdk v0.0.0-20180326062324-cfa1a18b161f // indirect
	github.com/creack/pty v1.1.9
	github.com/elastic/go-elasticsearch/v6 v6.8.5
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/gliderlabs/ssh v0.2.3-0.20190711180243-866d0ddf7991
	github.com/gorilla/mux v1.7.2
	github.com/gorilla/websocket v1.4.0
	github.com/jarcoal/httpmock v1.0.4
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/kataras/neffos v0.0.7
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/leonelquinteros/gotext v1.4.0
	github.com/mattn/go-runewidth v0.0.4 // indirect
	github.com/olekukonko/tablewriter v0.0.1
	github.com/pires/go-proxyproto v0.0.0-20190615163442-2c19fd512994
	github.com/pkg/errors v0.8.1
	github.com/pkg/sftp v1.10.0
	github.com/satori/go.uuid v1.2.0
	github.com/sevlyar/go-daemon v0.1.5
	github.com/sirupsen/logrus v1.4.2
	github.com/xlab/treeprint v0.0.0-20181112141820-a009c3971eca
	golang.org/x/crypto v0.0.0-20190820162420-60c769a6c586
	golang.org/x/text v0.3.1-0.20180807135948-17ff2d5776d2
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4 // indirect
	gopkg.in/go-playground/assert.v1 v1.2.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0-20170531160350-a96e63847dc3
	gopkg.in/yaml.v2 v2.2.2
)

replace (
	github.com/gliderlabs/ssh v0.2.3-0.20190711180243-866d0ddf7991 => github.com/ibuler/ssh v0.1.6-0.20191022095544-d805cc9f27a8
	github.com/pkg/sftp v1.10.0 => github.com/LeeEirc/sftp v1.10.2
	golang.org/x/crypto v0.0.0-20190820162420-60c769a6c586 => github.com/ibuler/crypto v0.0.0-20190715092645-911d13b3bf6e
)
