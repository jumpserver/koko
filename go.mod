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
	github.com/mediocregopher/radix/v3 v3.8.0
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
	github.com/StackExchange/wmi v0.0.0-20190523213315-cbe66965904d // indirect
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/baiyubin/aliyun-sts-go-sdk v0.0.0-20180326062324-cfa1a18b161f // indirect
	github.com/fsnotify/fsnotify v1.4.7 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/go-playground/form v3.1.4+incompatible // indirect
	github.com/go-playground/locales v0.14.0 // indirect
	github.com/go-playground/universal-translator v0.18.0 // indirect
	github.com/go-playground/validator/v10 v10.9.0 // indirect
	github.com/golang-sql/civil v0.0.0-20190719163853-cb61b32ac6fe // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/jmespath/go-jmespath v0.0.0-20180206201540-c2b33e8439af // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/leodido/go-urn v1.2.1 // indirect
	github.com/magiconair/properties v1.8.1 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/mitchellh/mapstructure v1.1.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml v1.2.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/spf13/afero v1.1.2 // indirect
	github.com/spf13/cast v1.3.0 // indirect
	github.com/spf13/jwalterweatherman v1.0.0 // indirect
	github.com/spf13/pflag v1.0.3 // indirect
	github.com/subosito/gotenv v1.2.0 // indirect
	github.com/ugorji/go/codec v1.2.6 // indirect
	golang.org/x/sys v0.0.0-20211111213525-f221eed1c01e // indirect
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/go-playground/assert.v1 v1.2.1 // indirect
	gopkg.in/ini.v1 v1.51.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

replace (
	github.com/gliderlabs/ssh v0.3.3 => github.com/LeeEirc/ssh v0.1.2-0.20211112094038-5ebcf34caaf1
	golang.org/x/crypto v0.0.0-20211108221036-ceb1ce70b4fa => github.com/LeeEirc/crypto v0.0.0-20211112090926-652515632c44
)
