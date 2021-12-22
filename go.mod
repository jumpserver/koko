module github.com/jumpserver/koko

go 1.17

require (
	github.com/Azure/azure-storage-blob-go v0.6.0
	github.com/LeeEirc/elfinder v0.0.14
	github.com/LeeEirc/tclientlib v0.0.1
	github.com/LeeEirc/terminalparser v0.0.0-20210105090630-135adbff588a
	github.com/aliyun/aliyun-oss-go-sdk v1.9.8
	github.com/aws/aws-sdk-go v1.19.46
	github.com/creack/pty v1.1.11
	github.com/denisenkom/go-mssqldb v0.11.0
	github.com/elastic/go-elasticsearch/v6 v6.8.5
	github.com/gin-gonic/gin v1.7.4
	github.com/gliderlabs/ssh v0.3.3
	github.com/go-sql-driver/mysql v1.6.0
	github.com/gorilla/websocket v1.4.2
	github.com/huaweicloud/huaweicloud-sdk-go-obs v3.21.1+incompatible
	github.com/jarcoal/httpmock v1.0.4
	github.com/leonelquinteros/gotext v1.4.0
	github.com/mediocregopher/radix/v3 v3.4.2
	github.com/olekukonko/tablewriter v0.0.1
	github.com/pires/go-proxyproto v0.0.0-20190615163442-2c19fd512994
	github.com/pkg/sftp v1.12.0
	github.com/satori/go.uuid v1.2.0
	github.com/sevlyar/go-daemon v0.1.5
	github.com/shirou/gopsutil/v3 v3.20.11
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/viper v1.7.1
	github.com/xlab/treeprint v0.0.0-20181112141820-a009c3971eca
	golang.org/x/crypto v0.0.0-20211108221036-ceb1ce70b4fa
	golang.org/x/text v0.3.7
	gopkg.in/natefinch/lumberjack.v2 v2.0.0-20170531160350-a96e63847dc3
	gopkg.in/twindagger/httpsig.v1 v1.2.0
)

require (
	github.com/Azure/azure-pipeline-go v0.1.9 // indirect
	github.com/baiyubin/aliyun-sts-go-sdk v0.0.0-20180326062324-cfa1a18b161f // indirect
	github.com/go-playground/validator/v10 v10.9.0 // indirect
	github.com/go-redis/redis v6.15.9+incompatible
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/onsi/gomega v1.17.0 // indirect
	github.com/ugorji/go v1.2.6 // indirect
	golang.org/x/sys v0.0.0-20211111213525-f221eed1c01e // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/go-playground/assert.v1 v1.2.1 // indirect
)

replace (
	github.com/gliderlabs/ssh v0.3.3 => github.com/LeeEirc/ssh v0.1.2-0.20211112094038-5ebcf34caaf1
	golang.org/x/crypto v0.0.0-20211108221036-ceb1ce70b4fa => github.com/LeeEirc/crypto v0.0.0-20211112090926-652515632c44
)
