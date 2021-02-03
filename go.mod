module github.com/jumpserver/koko

go 1.15

require (
	github.com/Azure/azure-pipeline-go v0.1.9 // indirect
	github.com/Azure/azure-storage-blob-go v0.6.0
	github.com/BurntSushi/toml v0.3.1 // indirect
	github.com/LeeEirc/elfinder v0.0.14
	github.com/LeeEirc/tclientlib v0.0.0-20201208031857-c210dd977a04
	github.com/LeeEirc/terminalparser v0.0.0-20210105090630-135adbff588a
	github.com/aliyun/aliyun-oss-go-sdk v1.9.8
	github.com/aws/aws-sdk-go v1.19.46
	github.com/baiyubin/aliyun-sts-go-sdk v0.0.0-20180326062324-cfa1a18b161f // indirect
	github.com/creack/pty v1.1.11
	github.com/elastic/go-elasticsearch/v6 v6.8.5
	github.com/gin-gonic/gin v1.6.3
	github.com/gliderlabs/ssh v0.2.3-0.20190711180243-866d0ddf7991
	github.com/gorilla/websocket v1.4.1
	github.com/jarcoal/httpmock v1.0.4
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/leonelquinteros/gotext v1.4.0
	github.com/mediocregopher/radix/v3 v3.4.2
	github.com/olekukonko/tablewriter v0.0.1
	github.com/pires/go-proxyproto v0.0.0-20190615163442-2c19fd512994
	github.com/pkg/sftp v1.12.0
	github.com/satori/go.uuid v1.2.0
	github.com/sevlyar/go-daemon v0.1.5
	github.com/shirou/gopsutil/v3 v3.20.11
	github.com/sirupsen/logrus v1.4.2
	github.com/xlab/treeprint v0.0.0-20181112141820-a009c3971eca
	golang.org/x/crypto v0.0.0-20201203163018-be400aefbc4c
	golang.org/x/sys v0.0.0-20201207223542-d4d67f95c62d // indirect
	golang.org/x/text v0.3.2
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4 // indirect
	gopkg.in/go-playground/assert.v1 v1.2.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0-20170531160350-a96e63847dc3
	gopkg.in/yaml.v2 v2.2.8
)

replace (
	github.com/gliderlabs/ssh v0.2.3-0.20190711180243-866d0ddf7991 => github.com/LeeEirc/ssh v0.1.2-0.20201111074515-e8272f1a6534
	golang.org/x/crypto v0.0.0-20201203163018-be400aefbc4c => github.com/LeeEirc/crypto v0.0.0-20201111063343-abd7a31f9aa8
)
