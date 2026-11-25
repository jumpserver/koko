/*
 * Copyright (c) 2000-2018, 达梦数据库有限公司.
 * All rights reserved.
 */
package dm

import (
	"bytes"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
	"io"
	"io/ioutil"
	"math"
)

type dm_build_942 struct{}

var Dm_build_943 = &dm_build_942{}

func (Dm_build_945 *dm_build_942) Dm_build_944(dm_build_946 []byte, dm_build_947 int, dm_build_948 byte) int {
	dm_build_946[dm_build_947] = dm_build_948
	return 1
}

func (Dm_build_950 *dm_build_942) Dm_build_949(dm_build_951 []byte, dm_build_952 int, dm_build_953 int8) int {
	dm_build_951[dm_build_952] = byte(dm_build_953)
	return 1
}

func (Dm_build_955 *dm_build_942) Dm_build_954(dm_build_956 []byte, dm_build_957 int, dm_build_958 int16) int {
	dm_build_956[dm_build_957] = byte(dm_build_958)
	dm_build_957++
	dm_build_956[dm_build_957] = byte(dm_build_958 >> 8)
	return 2
}

func (Dm_build_960 *dm_build_942) Dm_build_959(dm_build_961 []byte, dm_build_962 int, dm_build_963 int32) int {
	dm_build_961[dm_build_962] = byte(dm_build_963)
	dm_build_962++
	dm_build_961[dm_build_962] = byte(dm_build_963 >> 8)
	dm_build_962++
	dm_build_961[dm_build_962] = byte(dm_build_963 >> 16)
	dm_build_962++
	dm_build_961[dm_build_962] = byte(dm_build_963 >> 24)
	dm_build_962++
	return 4
}

func (Dm_build_965 *dm_build_942) Dm_build_964(dm_build_966 []byte, dm_build_967 int, dm_build_968 int64) int {
	dm_build_966[dm_build_967] = byte(dm_build_968)
	dm_build_967++
	dm_build_966[dm_build_967] = byte(dm_build_968 >> 8)
	dm_build_967++
	dm_build_966[dm_build_967] = byte(dm_build_968 >> 16)
	dm_build_967++
	dm_build_966[dm_build_967] = byte(dm_build_968 >> 24)
	dm_build_967++
	dm_build_966[dm_build_967] = byte(dm_build_968 >> 32)
	dm_build_967++
	dm_build_966[dm_build_967] = byte(dm_build_968 >> 40)
	dm_build_967++
	dm_build_966[dm_build_967] = byte(dm_build_968 >> 48)
	dm_build_967++
	dm_build_966[dm_build_967] = byte(dm_build_968 >> 56)
	return 8
}

func (Dm_build_970 *dm_build_942) Dm_build_969(dm_build_971 []byte, dm_build_972 int, dm_build_973 float32) int {
	return Dm_build_970.Dm_build_989(dm_build_971, dm_build_972, math.Float32bits(dm_build_973))
}

func (Dm_build_975 *dm_build_942) Dm_build_974(dm_build_976 []byte, dm_build_977 int, dm_build_978 float64) int {
	return Dm_build_975.Dm_build_994(dm_build_976, dm_build_977, math.Float64bits(dm_build_978))
}

func (Dm_build_980 *dm_build_942) Dm_build_979(dm_build_981 []byte, dm_build_982 int, dm_build_983 uint8) int {
	dm_build_981[dm_build_982] = byte(dm_build_983)
	return 1
}

func (Dm_build_985 *dm_build_942) Dm_build_984(dm_build_986 []byte, dm_build_987 int, dm_build_988 uint16) int {
	dm_build_986[dm_build_987] = byte(dm_build_988)
	dm_build_987++
	dm_build_986[dm_build_987] = byte(dm_build_988 >> 8)
	return 2
}

func (Dm_build_990 *dm_build_942) Dm_build_989(dm_build_991 []byte, dm_build_992 int, dm_build_993 uint32) int {
	dm_build_991[dm_build_992] = byte(dm_build_993)
	dm_build_992++
	dm_build_991[dm_build_992] = byte(dm_build_993 >> 8)
	dm_build_992++
	dm_build_991[dm_build_992] = byte(dm_build_993 >> 16)
	dm_build_992++
	dm_build_991[dm_build_992] = byte(dm_build_993 >> 24)
	return 3
}

func (Dm_build_995 *dm_build_942) Dm_build_994(dm_build_996 []byte, dm_build_997 int, dm_build_998 uint64) int {
	dm_build_996[dm_build_997] = byte(dm_build_998)
	dm_build_997++
	dm_build_996[dm_build_997] = byte(dm_build_998 >> 8)
	dm_build_997++
	dm_build_996[dm_build_997] = byte(dm_build_998 >> 16)
	dm_build_997++
	dm_build_996[dm_build_997] = byte(dm_build_998 >> 24)
	dm_build_997++
	dm_build_996[dm_build_997] = byte(dm_build_998 >> 32)
	dm_build_997++
	dm_build_996[dm_build_997] = byte(dm_build_998 >> 40)
	dm_build_997++
	dm_build_996[dm_build_997] = byte(dm_build_998 >> 48)
	dm_build_997++
	dm_build_996[dm_build_997] = byte(dm_build_998 >> 56)
	return 3
}

func (Dm_build_1000 *dm_build_942) Dm_build_999(dm_build_1001 []byte, dm_build_1002 int, dm_build_1003 []byte, dm_build_1004 int, dm_build_1005 int) int {
	copy(dm_build_1001[dm_build_1002:dm_build_1002+dm_build_1005], dm_build_1003[dm_build_1004:dm_build_1004+dm_build_1005])
	return dm_build_1005
}

func (Dm_build_1007 *dm_build_942) Dm_build_1006(dm_build_1008 []byte, dm_build_1009 int, dm_build_1010 []byte, dm_build_1011 int, dm_build_1012 int) int {
	dm_build_1009 += Dm_build_1007.Dm_build_989(dm_build_1008, dm_build_1009, uint32(dm_build_1012))
	return 4 + Dm_build_1007.Dm_build_999(dm_build_1008, dm_build_1009, dm_build_1010, dm_build_1011, dm_build_1012)
}

func (Dm_build_1014 *dm_build_942) Dm_build_1013(dm_build_1015 []byte, dm_build_1016 int, dm_build_1017 []byte, dm_build_1018 int, dm_build_1019 int) int {
	dm_build_1016 += Dm_build_1014.Dm_build_984(dm_build_1015, dm_build_1016, uint16(dm_build_1019))
	return 2 + Dm_build_1014.Dm_build_999(dm_build_1015, dm_build_1016, dm_build_1017, dm_build_1018, dm_build_1019)
}

func (Dm_build_1021 *dm_build_942) Dm_build_1020(dm_build_1022 []byte, dm_build_1023 int, dm_build_1024 string, dm_build_1025 string, dm_build_1026 *DmConnection) int {
	dm_build_1027 := Dm_build_1021.Dm_build_1159(dm_build_1024, dm_build_1025, dm_build_1026)
	dm_build_1023 += Dm_build_1021.Dm_build_989(dm_build_1022, dm_build_1023, uint32(len(dm_build_1027)))
	return 4 + Dm_build_1021.Dm_build_999(dm_build_1022, dm_build_1023, dm_build_1027, 0, len(dm_build_1027))
}

func (Dm_build_1029 *dm_build_942) Dm_build_1028(dm_build_1030 []byte, dm_build_1031 int, dm_build_1032 string, dm_build_1033 string, dm_build_1034 *DmConnection) int {
	dm_build_1035 := Dm_build_1029.Dm_build_1159(dm_build_1032, dm_build_1033, dm_build_1034)

	dm_build_1031 += Dm_build_1029.Dm_build_984(dm_build_1030, dm_build_1031, uint16(len(dm_build_1035)))
	return 2 + Dm_build_1029.Dm_build_999(dm_build_1030, dm_build_1031, dm_build_1035, 0, len(dm_build_1035))
}

func (Dm_build_1037 *dm_build_942) Dm_build_1036(dm_build_1038 []byte, dm_build_1039 int) byte {
	return dm_build_1038[dm_build_1039]
}

func (Dm_build_1041 *dm_build_942) Dm_build_1040(dm_build_1042 []byte, dm_build_1043 int) int16 {
	var dm_build_1044 int16
	dm_build_1044 = int16(dm_build_1042[dm_build_1043] & 0xff)
	dm_build_1043++
	dm_build_1044 |= int16(dm_build_1042[dm_build_1043]&0xff) << 8
	return dm_build_1044
}

func (Dm_build_1046 *dm_build_942) Dm_build_1045(dm_build_1047 []byte, dm_build_1048 int) int32 {
	var dm_build_1049 int32
	dm_build_1049 = int32(dm_build_1047[dm_build_1048] & 0xff)
	dm_build_1048++
	dm_build_1049 |= int32(dm_build_1047[dm_build_1048]&0xff) << 8
	dm_build_1048++
	dm_build_1049 |= int32(dm_build_1047[dm_build_1048]&0xff) << 16
	dm_build_1048++
	dm_build_1049 |= int32(dm_build_1047[dm_build_1048]&0xff) << 24
	return dm_build_1049
}

func (Dm_build_1051 *dm_build_942) Dm_build_1050(dm_build_1052 []byte, dm_build_1053 int) int64 {
	var dm_build_1054 int64
	dm_build_1054 = int64(dm_build_1052[dm_build_1053] & 0xff)
	dm_build_1053++
	dm_build_1054 |= int64(dm_build_1052[dm_build_1053]&0xff) << 8
	dm_build_1053++
	dm_build_1054 |= int64(dm_build_1052[dm_build_1053]&0xff) << 16
	dm_build_1053++
	dm_build_1054 |= int64(dm_build_1052[dm_build_1053]&0xff) << 24
	dm_build_1053++
	dm_build_1054 |= int64(dm_build_1052[dm_build_1053]&0xff) << 32
	dm_build_1053++
	dm_build_1054 |= int64(dm_build_1052[dm_build_1053]&0xff) << 40
	dm_build_1053++
	dm_build_1054 |= int64(dm_build_1052[dm_build_1053]&0xff) << 48
	dm_build_1053++
	dm_build_1054 |= int64(dm_build_1052[dm_build_1053]&0xff) << 56
	return dm_build_1054
}

func (Dm_build_1056 *dm_build_942) Dm_build_1055(dm_build_1057 []byte, dm_build_1058 int) float32 {
	return math.Float32frombits(Dm_build_1056.Dm_build_1072(dm_build_1057, dm_build_1058))
}

func (Dm_build_1060 *dm_build_942) Dm_build_1059(dm_build_1061 []byte, dm_build_1062 int) float64 {
	return math.Float64frombits(Dm_build_1060.Dm_build_1077(dm_build_1061, dm_build_1062))
}

func (Dm_build_1064 *dm_build_942) Dm_build_1063(dm_build_1065 []byte, dm_build_1066 int) uint8 {
	return uint8(dm_build_1065[dm_build_1066] & 0xff)
}

func (Dm_build_1068 *dm_build_942) Dm_build_1067(dm_build_1069 []byte, dm_build_1070 int) uint16 {
	var dm_build_1071 uint16
	dm_build_1071 = uint16(dm_build_1069[dm_build_1070] & 0xff)
	dm_build_1070++
	dm_build_1071 |= uint16(dm_build_1069[dm_build_1070]&0xff) << 8
	return dm_build_1071
}

func (Dm_build_1073 *dm_build_942) Dm_build_1072(dm_build_1074 []byte, dm_build_1075 int) uint32 {
	var dm_build_1076 uint32
	dm_build_1076 = uint32(dm_build_1074[dm_build_1075] & 0xff)
	dm_build_1075++
	dm_build_1076 |= uint32(dm_build_1074[dm_build_1075]&0xff) << 8
	dm_build_1075++
	dm_build_1076 |= uint32(dm_build_1074[dm_build_1075]&0xff) << 16
	dm_build_1075++
	dm_build_1076 |= uint32(dm_build_1074[dm_build_1075]&0xff) << 24
	return dm_build_1076
}

func (Dm_build_1078 *dm_build_942) Dm_build_1077(dm_build_1079 []byte, dm_build_1080 int) uint64 {
	var dm_build_1081 uint64
	dm_build_1081 = uint64(dm_build_1079[dm_build_1080] & 0xff)
	dm_build_1080++
	dm_build_1081 |= uint64(dm_build_1079[dm_build_1080]&0xff) << 8
	dm_build_1080++
	dm_build_1081 |= uint64(dm_build_1079[dm_build_1080]&0xff) << 16
	dm_build_1080++
	dm_build_1081 |= uint64(dm_build_1079[dm_build_1080]&0xff) << 24
	dm_build_1080++
	dm_build_1081 |= uint64(dm_build_1079[dm_build_1080]&0xff) << 32
	dm_build_1080++
	dm_build_1081 |= uint64(dm_build_1079[dm_build_1080]&0xff) << 40
	dm_build_1080++
	dm_build_1081 |= uint64(dm_build_1079[dm_build_1080]&0xff) << 48
	dm_build_1080++
	dm_build_1081 |= uint64(dm_build_1079[dm_build_1080]&0xff) << 56
	return dm_build_1081
}

func (Dm_build_1083 *dm_build_942) Dm_build_1082(dm_build_1084 []byte, dm_build_1085 int) []byte {
	dm_build_1086 := Dm_build_1083.Dm_build_1072(dm_build_1084, dm_build_1085)

	dm_build_1087 := make([]byte, dm_build_1086)
	copy(dm_build_1087[:int(dm_build_1086)], dm_build_1084[dm_build_1085+4:dm_build_1085+4+int(dm_build_1086)])
	return dm_build_1087
}

func (Dm_build_1089 *dm_build_942) Dm_build_1088(dm_build_1090 []byte, dm_build_1091 int) []byte {
	dm_build_1092 := Dm_build_1089.Dm_build_1067(dm_build_1090, dm_build_1091)

	dm_build_1093 := make([]byte, dm_build_1092)
	copy(dm_build_1093[:int(dm_build_1092)], dm_build_1090[dm_build_1091+2:dm_build_1091+2+int(dm_build_1092)])
	return dm_build_1093
}

func (Dm_build_1095 *dm_build_942) Dm_build_1094(dm_build_1096 []byte, dm_build_1097 int, dm_build_1098 int) []byte {

	dm_build_1099 := make([]byte, dm_build_1098)
	copy(dm_build_1099[:dm_build_1098], dm_build_1096[dm_build_1097:dm_build_1097+dm_build_1098])
	return dm_build_1099
}

func (Dm_build_1101 *dm_build_942) Dm_build_1100(dm_build_1102 []byte, dm_build_1103 int, dm_build_1104 int, dm_build_1105 string, dm_build_1106 *DmConnection) string {
	return Dm_build_1101.Dm_build_1195(dm_build_1102[dm_build_1103:dm_build_1103+dm_build_1104], dm_build_1105, dm_build_1106)
}

func (Dm_build_1108 *dm_build_942) Dm_build_1107(dm_build_1109 []byte, dm_build_1110 int, dm_build_1111 string, dm_build_1112 *DmConnection) string {
	dm_build_1113 := Dm_build_1108.Dm_build_1072(dm_build_1109, dm_build_1110)
	dm_build_1110 += 4
	return Dm_build_1108.Dm_build_1100(dm_build_1109, dm_build_1110, int(dm_build_1113), dm_build_1111, dm_build_1112)
}

func (Dm_build_1115 *dm_build_942) Dm_build_1114(dm_build_1116 []byte, dm_build_1117 int, dm_build_1118 string, dm_build_1119 *DmConnection) string {
	dm_build_1120 := Dm_build_1115.Dm_build_1067(dm_build_1116, dm_build_1117)
	dm_build_1117 += 2
	return Dm_build_1115.Dm_build_1100(dm_build_1116, dm_build_1117, int(dm_build_1120), dm_build_1118, dm_build_1119)
}

func (Dm_build_1122 *dm_build_942) Dm_build_1121(dm_build_1123 byte) []byte {
	return []byte{dm_build_1123}
}

func (Dm_build_1125 *dm_build_942) Dm_build_1124(dm_build_1126 int8) []byte {
	return []byte{byte(dm_build_1126)}
}

func (Dm_build_1128 *dm_build_942) Dm_build_1127(dm_build_1129 int16) []byte {
	return []byte{byte(dm_build_1129), byte(dm_build_1129 >> 8)}
}

func (Dm_build_1131 *dm_build_942) Dm_build_1130(dm_build_1132 int32) []byte {
	return []byte{byte(dm_build_1132), byte(dm_build_1132 >> 8), byte(dm_build_1132 >> 16), byte(dm_build_1132 >> 24)}
}

func (Dm_build_1134 *dm_build_942) Dm_build_1133(dm_build_1135 int64) []byte {
	return []byte{byte(dm_build_1135), byte(dm_build_1135 >> 8), byte(dm_build_1135 >> 16), byte(dm_build_1135 >> 24), byte(dm_build_1135 >> 32),
		byte(dm_build_1135 >> 40), byte(dm_build_1135 >> 48), byte(dm_build_1135 >> 56)}
}

func (Dm_build_1137 *dm_build_942) Dm_build_1136(dm_build_1138 float32) []byte {
	return Dm_build_1137.Dm_build_1148(math.Float32bits(dm_build_1138))
}

func (Dm_build_1140 *dm_build_942) Dm_build_1139(dm_build_1141 float64) []byte {
	return Dm_build_1140.Dm_build_1151(math.Float64bits(dm_build_1141))
}

func (Dm_build_1143 *dm_build_942) Dm_build_1142(dm_build_1144 uint8) []byte {
	return []byte{byte(dm_build_1144)}
}

func (Dm_build_1146 *dm_build_942) Dm_build_1145(dm_build_1147 uint16) []byte {
	return []byte{byte(dm_build_1147), byte(dm_build_1147 >> 8)}
}

func (Dm_build_1149 *dm_build_942) Dm_build_1148(dm_build_1150 uint32) []byte {
	return []byte{byte(dm_build_1150), byte(dm_build_1150 >> 8), byte(dm_build_1150 >> 16), byte(dm_build_1150 >> 24)}
}

func (Dm_build_1152 *dm_build_942) Dm_build_1151(dm_build_1153 uint64) []byte {
	return []byte{byte(dm_build_1153), byte(dm_build_1153 >> 8), byte(dm_build_1153 >> 16), byte(dm_build_1153 >> 24), byte(dm_build_1153 >> 32), byte(dm_build_1153 >> 40), byte(dm_build_1153 >> 48), byte(dm_build_1153 >> 56)}
}

func (Dm_build_1155 *dm_build_942) Dm_build_1154(dm_build_1156 []byte, dm_build_1157 string, dm_build_1158 *DmConnection) []byte {
	if dm_build_1157 == "UTF-8" {
		return dm_build_1156
	}

	if dm_build_1158 == nil {
		if e := dm_build_1200(dm_build_1157); e != nil {
			tmp, err := ioutil.ReadAll(
				transform.NewReader(bytes.NewReader(dm_build_1156), e.NewEncoder()),
			)
			if err != nil {
				panic("UTF8 To Charset error!")
			}

			return tmp
		}

		panic("Unsupported Charset!")
	}

	if dm_build_1158.encodeBuffer == nil {
		dm_build_1158.encodeBuffer = bytes.NewBuffer(nil)
		dm_build_1158.encode = dm_build_1200(dm_build_1158.getServerEncoding())
		dm_build_1158.transformReaderDst = make([]byte, 4096)
		dm_build_1158.transformReaderSrc = make([]byte, 4096)
	}

	if e := dm_build_1158.encode; e != nil {

		dm_build_1158.encodeBuffer.Reset()

		n, err := dm_build_1158.encodeBuffer.ReadFrom(
			Dm_build_1214(bytes.NewReader(dm_build_1156), e.NewEncoder(), dm_build_1158.transformReaderDst, dm_build_1158.transformReaderSrc),
		)
		if err != nil {
			panic("UTF8 To Charset error!")
		}
		var tmp = make([]byte, n)
		if _, err = dm_build_1158.encodeBuffer.Read(tmp); err != nil {
			panic("UTF8 To Charset error!")
		}
		return tmp
	}

	panic("Unsupported Charset!")
}

func (Dm_build_1160 *dm_build_942) Dm_build_1159(dm_build_1161 string, dm_build_1162 string, dm_build_1163 *DmConnection) []byte {
	return Dm_build_1160.Dm_build_1154([]byte(dm_build_1161), dm_build_1162, dm_build_1163)
}

func (Dm_build_1165 *dm_build_942) Dm_build_1164(dm_build_1166 []byte) byte {
	return Dm_build_1165.Dm_build_1036(dm_build_1166, 0)
}

func (Dm_build_1168 *dm_build_942) Dm_build_1167(dm_build_1169 []byte) int16 {
	return Dm_build_1168.Dm_build_1040(dm_build_1169, 0)
}

func (Dm_build_1171 *dm_build_942) Dm_build_1170(dm_build_1172 []byte) int32 {
	return Dm_build_1171.Dm_build_1045(dm_build_1172, 0)
}

func (Dm_build_1174 *dm_build_942) Dm_build_1173(dm_build_1175 []byte) int64 {
	return Dm_build_1174.Dm_build_1050(dm_build_1175, 0)
}

func (Dm_build_1177 *dm_build_942) Dm_build_1176(dm_build_1178 []byte) float32 {
	return Dm_build_1177.Dm_build_1055(dm_build_1178, 0)
}

func (Dm_build_1180 *dm_build_942) Dm_build_1179(dm_build_1181 []byte) float64 {
	return Dm_build_1180.Dm_build_1059(dm_build_1181, 0)
}

func (Dm_build_1183 *dm_build_942) Dm_build_1182(dm_build_1184 []byte) uint8 {
	return Dm_build_1183.Dm_build_1063(dm_build_1184, 0)
}

func (Dm_build_1186 *dm_build_942) Dm_build_1185(dm_build_1187 []byte) uint16 {
	return Dm_build_1186.Dm_build_1067(dm_build_1187, 0)
}

func (Dm_build_1189 *dm_build_942) Dm_build_1188(dm_build_1190 []byte) uint32 {
	return Dm_build_1189.Dm_build_1072(dm_build_1190, 0)
}

func (Dm_build_1192 *dm_build_942) Dm_build_1191(dm_build_1193 []byte, dm_build_1194 string) []byte {
	if dm_build_1194 == "UTF-8" {
		return dm_build_1193
	}

	if e := dm_build_1200(dm_build_1194); e != nil {

		tmp, err := ioutil.ReadAll(
			transform.NewReader(bytes.NewReader(dm_build_1193), e.NewDecoder()),
		)
		if err != nil {

			panic("Charset To UTF8 error!")
		}

		return tmp
	}

	panic("Unsupported Charset!")

}

func (Dm_build_1196 *dm_build_942) Dm_build_1195(dm_build_1197 []byte, dm_build_1198 string, dm_build_1199 *DmConnection) string {
	return string(Dm_build_1196.Dm_build_1191(dm_build_1197, dm_build_1198))
}

func dm_build_1200(dm_build_1201 string) encoding.Encoding {
	if e, err := ianaindex.MIB.Encoding(dm_build_1201); err == nil && e != nil {
		return e
	}
	return nil
}

type Dm_build_1202 struct {
	dm_build_1203 io.Reader
	dm_build_1204 transform.Transformer
	dm_build_1205 error

	dm_build_1206                []byte
	dm_build_1207, dm_build_1208 int

	dm_build_1209                []byte
	dm_build_1210, dm_build_1211 int

	dm_build_1212 bool
}

const dm_build_1213 = 4096

func Dm_build_1214(dm_build_1215 io.Reader, dm_build_1216 transform.Transformer, dm_build_1217 []byte, dm_build_1218 []byte) *Dm_build_1202 {
	dm_build_1216.Reset()
	return &Dm_build_1202{
		dm_build_1203: dm_build_1215,
		dm_build_1204: dm_build_1216,
		dm_build_1206: dm_build_1217,
		dm_build_1209: dm_build_1218,
	}
}

func (dm_build_1220 *Dm_build_1202) Read(dm_build_1221 []byte) (int, error) {
	dm_build_1222, dm_build_1223 := 0, error(nil)
	for {

		if dm_build_1220.dm_build_1207 != dm_build_1220.dm_build_1208 {
			dm_build_1222 = copy(dm_build_1221, dm_build_1220.dm_build_1206[dm_build_1220.dm_build_1207:dm_build_1220.dm_build_1208])
			dm_build_1220.dm_build_1207 += dm_build_1222
			if dm_build_1220.dm_build_1207 == dm_build_1220.dm_build_1208 && dm_build_1220.dm_build_1212 {
				return dm_build_1222, dm_build_1220.dm_build_1205
			}
			return dm_build_1222, nil
		} else if dm_build_1220.dm_build_1212 {
			return 0, dm_build_1220.dm_build_1205
		}

		if dm_build_1220.dm_build_1210 != dm_build_1220.dm_build_1211 || dm_build_1220.dm_build_1205 != nil {
			dm_build_1220.dm_build_1207 = 0
			dm_build_1220.dm_build_1208, dm_build_1222, dm_build_1223 = dm_build_1220.dm_build_1204.Transform(dm_build_1220.dm_build_1206, dm_build_1220.dm_build_1209[dm_build_1220.dm_build_1210:dm_build_1220.dm_build_1211], dm_build_1220.dm_build_1205 == io.EOF)
			dm_build_1220.dm_build_1210 += dm_build_1222

			switch {
			case dm_build_1223 == nil:
				if dm_build_1220.dm_build_1210 != dm_build_1220.dm_build_1211 {
					dm_build_1220.dm_build_1205 = nil
				}

				dm_build_1220.dm_build_1212 = dm_build_1220.dm_build_1205 != nil
				continue
			case dm_build_1223 == transform.ErrShortDst && (dm_build_1220.dm_build_1208 != 0 || dm_build_1222 != 0):

				continue
			case dm_build_1223 == transform.ErrShortSrc && dm_build_1220.dm_build_1211-dm_build_1220.dm_build_1210 != len(dm_build_1220.dm_build_1209) && dm_build_1220.dm_build_1205 == nil:

			default:
				dm_build_1220.dm_build_1212 = true

				if dm_build_1220.dm_build_1205 == nil || dm_build_1220.dm_build_1205 == io.EOF {
					dm_build_1220.dm_build_1205 = dm_build_1223
				}
				continue
			}
		}

		if dm_build_1220.dm_build_1210 != 0 {
			dm_build_1220.dm_build_1210, dm_build_1220.dm_build_1211 = 0, copy(dm_build_1220.dm_build_1209, dm_build_1220.dm_build_1209[dm_build_1220.dm_build_1210:dm_build_1220.dm_build_1211])
		}
		dm_build_1222, dm_build_1220.dm_build_1205 = dm_build_1220.dm_build_1203.Read(dm_build_1220.dm_build_1209[dm_build_1220.dm_build_1211:])
		dm_build_1220.dm_build_1211 += dm_build_1222
	}
}
