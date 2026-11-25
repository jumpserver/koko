/*
 * Copyright (c) 2000-2018, 达梦数据库有限公司.
 * All rights reserved.
 */
package dm

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"
	"unicode/utf8"

	"gitee.com/chunanyong/dm/security"
)

const (
	Dm_build_0 = 8192
	Dm_build_1 = 2 * time.Second
)

type dm_build_2 struct {
	dm_build_3  net.Conn
	dm_build_4  *tls.Conn
	dm_build_5  *Dm_build_1302
	dm_build_6  *DmConnection
	dm_build_7  security.Cipher
	dm_build_8  bool
	dm_build_9  bool
	dm_build_10 *security.DhKey

	dm_build_11 bool
	dm_build_12 string
	dm_build_13 bool
}

func dm_build_14(dm_build_15 context.Context, dm_build_16 *DmConnection) (*dm_build_2, error) {
	var dm_build_17 net.Conn
	var dm_build_18 error

	dialsLock.RLock()
	dm_build_19, dm_build_20 := dials[dm_build_16.dmConnector.dialName]
	dialsLock.RUnlock()
	if dm_build_20 {
		dm_build_17, dm_build_18 = dm_build_19(dm_build_15, dm_build_16.dmConnector.host+":"+strconv.Itoa(int(dm_build_16.dmConnector.port)))
	} else {
		dm_build_17, dm_build_18 = dm_build_22(dm_build_16.dmConnector.host+":"+strconv.Itoa(int(dm_build_16.dmConnector.port)), time.Duration(dm_build_16.dmConnector.socketTimeout)*time.Second)
	}
	if dm_build_18 != nil {
		return nil, dm_build_18
	}

	dm_build_21 := dm_build_2{}
	dm_build_21.dm_build_3 = dm_build_17
	dm_build_21.dm_build_5 = Dm_build_1305(Dm_build_297)
	dm_build_21.dm_build_6 = dm_build_16
	dm_build_21.dm_build_8 = false
	dm_build_21.dm_build_9 = false
	dm_build_21.dm_build_11 = false
	dm_build_21.dm_build_12 = ""
	dm_build_21.dm_build_13 = false
	dm_build_16.Access = &dm_build_21

	return &dm_build_21, nil
}

func dm_build_22(dm_build_23 string, dm_build_24 time.Duration) (net.Conn, error) {
	dm_build_25, dm_build_26 := net.DialTimeout("tcp", dm_build_23, dm_build_24)
	if dm_build_26 != nil {
		return &net.TCPConn{}, ECGO_COMMUNITION_ERROR.addDetail("\tdial address: " + dm_build_23).throw()
	}

	if tcpConn, ok := dm_build_25.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(Dm_build_1)
		tcpConn.SetNoDelay(true)

	}
	return dm_build_25, nil
}

func (dm_build_28 *dm_build_2) dm_build_27(dm_build_29 dm_build_419) bool {
	var dm_build_30 = dm_build_28.dm_build_6.dmConnector.compress
	if dm_build_29.dm_build_434() == Dm_build_325 || dm_build_30 == Dm_build_374 {
		return false
	}

	if dm_build_30 == Dm_build_372 {
		return true
	} else if dm_build_30 == Dm_build_373 {
		return !dm_build_28.dm_build_6.Local && dm_build_29.dm_build_432() > Dm_build_371
	}

	return false
}

func (dm_build_32 *dm_build_2) dm_build_31(dm_build_33 dm_build_419) bool {
	var dm_build_34 = dm_build_32.dm_build_6.dmConnector.compress
	if dm_build_33.dm_build_434() == Dm_build_325 || dm_build_34 == Dm_build_374 {
		return false
	}

	if dm_build_34 == Dm_build_372 {
		return true
	} else if dm_build_34 == Dm_build_373 {
		return dm_build_32.dm_build_5.Dm_build_1569(Dm_build_333) == 1
	}

	return false
}

func (dm_build_36 *dm_build_2) dm_build_35(dm_build_37 dm_build_419) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(string); ok {
				err = ECGO_COMMUNITION_ERROR.addDetail("\t" + p.(string)).throw()
			} else {
				err = fmt.Errorf("internal error: %v", p)
			}
		}
	}()

	dm_build_39 := dm_build_37.dm_build_432()

	if dm_build_39 > 0 {

		if dm_build_36.dm_build_27(dm_build_37) {
			var retBytes, err = Compress(dm_build_36.dm_build_5, Dm_build_326, int(dm_build_39), int(dm_build_36.dm_build_6.dmConnector.compressID))
			if err != nil {
				return err
			}

			dm_build_36.dm_build_5.Dm_build_1316(Dm_build_326)

			dm_build_36.dm_build_5.Dm_build_1357(dm_build_39)

			dm_build_36.dm_build_5.Dm_build_1385(retBytes)

			dm_build_37.dm_build_433(int32(len(retBytes)) + ULINT_SIZE)

			dm_build_36.dm_build_5.Dm_build_1489(Dm_build_333, 1)
		}

		if dm_build_36.dm_build_9 {
			dm_build_39 = dm_build_37.dm_build_432()
			var retBytes = dm_build_36.dm_build_7.Encrypt(dm_build_36.dm_build_5.Dm_build_1596(Dm_build_326, int(dm_build_39)), true)

			dm_build_36.dm_build_5.Dm_build_1316(Dm_build_326)

			dm_build_36.dm_build_5.Dm_build_1385(retBytes)

			dm_build_37.dm_build_433(int32(len(retBytes)))
		}
	}

	if dm_build_36.dm_build_5.Dm_build_1314() > Dm_build_298 {
		return ECGO_MSG_TOO_LONG.throw()
	}

	dm_build_37.dm_build_428()
	if dm_build_36.dm_build_279(dm_build_37) {
		if dm_build_36.dm_build_4 != nil {
			dm_build_36.dm_build_5.Dm_build_1319(0)
			if _, err := dm_build_36.dm_build_5.Dm_build_1338(dm_build_36.dm_build_4); err != nil {
				return err
			}
		}
	} else {
		dm_build_36.dm_build_5.Dm_build_1319(0)
		if _, err := dm_build_36.dm_build_5.Dm_build_1338(dm_build_36.dm_build_3); err != nil {
			return err
		}
	}
	return nil
}

func (dm_build_41 *dm_build_2) dm_build_40(dm_build_42 dm_build_419) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(string); ok {
				err = ECGO_COMMUNITION_ERROR.addDetail("\t" + p.(string)).throw()
			} else {
				err = fmt.Errorf("internal error: %v", p)
			}
		}
	}()

	dm_build_44 := int32(0)
	if dm_build_41.dm_build_279(dm_build_42) {
		if dm_build_41.dm_build_4 != nil {
			dm_build_41.dm_build_5.Dm_build_1316(0)
			if _, err := dm_build_41.dm_build_5.Dm_build_1332(dm_build_41.dm_build_4, Dm_build_326); err != nil {
				return err
			}

			dm_build_44 = dm_build_42.dm_build_432()
			if dm_build_44 > 0 {
				if _, err := dm_build_41.dm_build_5.Dm_build_1332(dm_build_41.dm_build_4, int(dm_build_44)); err != nil {
					return err
				}
			}
		}
	} else {

		dm_build_41.dm_build_5.Dm_build_1316(0)
		if _, err := dm_build_41.dm_build_5.Dm_build_1332(dm_build_41.dm_build_3, Dm_build_326); err != nil {
			return err
		}
		dm_build_44 = dm_build_42.dm_build_432()

		if dm_build_44 > 0 {
			if _, err := dm_build_41.dm_build_5.Dm_build_1332(dm_build_41.dm_build_3, int(dm_build_44)); err != nil {
				return err
			}
		}
	}

	dm_build_42.dm_build_429()

	dm_build_44 = dm_build_42.dm_build_432()
	if dm_build_44 <= 0 {
		return nil
	}

	if dm_build_41.dm_build_9 {
		ebytes := dm_build_41.dm_build_5.Dm_build_1596(Dm_build_326, int(dm_build_44))
		bytes, err := dm_build_41.dm_build_7.Decrypt(ebytes, true)
		if err != nil {
			return err
		}
		dm_build_41.dm_build_5.Dm_build_1316(Dm_build_326)
		dm_build_41.dm_build_5.Dm_build_1385(bytes)
		dm_build_42.dm_build_433(int32(len(bytes)))
	}

	if dm_build_41.dm_build_31(dm_build_42) {

		dm_build_44 = dm_build_42.dm_build_432()
		cbytes := dm_build_41.dm_build_5.Dm_build_1596(Dm_build_326+ULINT_SIZE, int(dm_build_44-ULINT_SIZE))
		bytes, err := UnCompress(cbytes, int(dm_build_41.dm_build_6.dmConnector.compressID))
		if err != nil {
			return err
		}
		dm_build_41.dm_build_5.Dm_build_1316(Dm_build_326)
		dm_build_41.dm_build_5.Dm_build_1385(bytes)
		dm_build_42.dm_build_433(int32(len(bytes)))
	}
	return nil
}

func (dm_build_46 *dm_build_2) dm_build_45(dm_build_47 dm_build_419) (dm_build_48 interface{}, dm_build_49 error) {
	if dm_build_46.dm_build_13 {
		return nil, ECGO_CONNECTION_CLOSED.throw()
	}
	dm_build_50 := dm_build_46.dm_build_6
	dm_build_50.mu.Lock()
	defer dm_build_50.mu.Unlock()
	dm_build_49 = dm_build_47.dm_build_423(dm_build_47)
	if dm_build_49 != nil {
		return nil, dm_build_49
	}

	dm_build_49 = dm_build_46.dm_build_35(dm_build_47)
	if dm_build_49 != nil {
		return nil, dm_build_49
	}

	dm_build_49 = dm_build_46.dm_build_40(dm_build_47)
	if dm_build_49 != nil {
		return nil, dm_build_49
	}

	return dm_build_47.dm_build_427(dm_build_47)
}

func (dm_build_52 *dm_build_2) dm_build_51() (*dm_build_881, error) {

	Dm_build_53 := dm_build_887(dm_build_52)
	_, dm_build_54 := dm_build_52.dm_build_45(Dm_build_53)
	if dm_build_54 != nil {
		return nil, dm_build_54
	}

	return Dm_build_53, nil
}

func (dm_build_56 *dm_build_2) dm_build_55() error {

	dm_build_57 := dm_build_745(dm_build_56)
	_, dm_build_58 := dm_build_56.dm_build_45(dm_build_57)
	if dm_build_58 != nil {
		return dm_build_58
	}

	return nil
}

func (dm_build_60 *dm_build_2) dm_build_59() error {

	var dm_build_61 *dm_build_881
	var err error
	if dm_build_61, err = dm_build_60.dm_build_51(); err != nil {
		return err
	}

	if dm_build_60.dm_build_6.sslEncrypt == 2 {
		if err = dm_build_60.dm_build_274(false, true); err != nil {
			return ECGO_INIT_SSL_FAILED.addDetail("\n" + err.Error()).throw()
		}
	} else if dm_build_60.dm_build_6.sslEncrypt == 1 || dm_build_60.dm_build_6.sslEncrypt == 4 {
		if err = dm_build_60.dm_build_274(true, false); err != nil {
			return ECGO_INIT_SSL_FAILED.addDetail("\n" + err.Error()).throw()
		}
	}

	if dm_build_60.dm_build_9 || dm_build_60.dm_build_8 {
		k, err := dm_build_60.dm_build_264()
		if err != nil {
			return err
		}
		sessionKey := security.ComputeSessionKey(k, dm_build_61.Dm_build_885)
		encryptType := dm_build_61.dm_build_883
		hashType := int(dm_build_61.Dm_build_884)
		if encryptType == -1 {
			encryptType = security.DES_CFB
		}
		if hashType == -1 {
			hashType = security.MD5
		}
		err = dm_build_60.dm_build_267(encryptType, sessionKey, dm_build_60.dm_build_6.dmConnector.cipherPath, hashType)
		if err != nil {
			return err
		}
	}

	if err := dm_build_60.dm_build_55(); err != nil {
		return err
	}
	return nil
}

func (dm_build_64 *dm_build_2) Dm_build_63(dm_build_65 *DmStatement) error {
	dm_build_66 := dm_build_914(dm_build_64, dm_build_65)
	_, dm_build_67 := dm_build_64.dm_build_45(dm_build_66)
	if dm_build_67 != nil {
		return dm_build_67
	}

	return nil
}

func (dm_build_69 *dm_build_2) Dm_build_68(dm_build_70 int32) error {
	dm_build_71 := dm_build_924(dm_build_69, dm_build_70)
	_, dm_build_72 := dm_build_69.dm_build_45(dm_build_71)
	if dm_build_72 != nil {
		return dm_build_72
	}

	return nil
}

func (dm_build_74 *dm_build_2) Dm_build_73(dm_build_75 *DmStatement, dm_build_76 bool, dm_build_77 int16) (*execRetInfo, error) {
	dm_build_78 := dm_build_787(dm_build_74, dm_build_75, dm_build_76, dm_build_77)
	dm_build_79, dm_build_80 := dm_build_74.dm_build_45(dm_build_78)
	if dm_build_80 != nil {
		return nil, dm_build_80
	}
	return dm_build_79.(*execRetInfo), nil
}

func (dm_build_82 *dm_build_2) Dm_build_81(dm_build_83 *DmStatement, dm_build_84 int16) (*execRetInfo, error) {
	return dm_build_82.Dm_build_73(dm_build_83, false, Dm_build_378)
}

func (dm_build_86 *dm_build_2) Dm_build_85(dm_build_87 *DmStatement, dm_build_88 []OptParameter) (*execRetInfo, error) {
	dm_build_89, dm_build_90 := dm_build_86.dm_build_45(dm_build_522(dm_build_86, dm_build_87, dm_build_88))
	if dm_build_90 != nil {
		return nil, dm_build_90
	}

	return dm_build_89.(*execRetInfo), nil
}

func (dm_build_92 *dm_build_2) Dm_build_91(dm_build_93 *DmStatement, dm_build_94 int16) (*execRetInfo, error) {
	return dm_build_92.Dm_build_73(dm_build_93, true, dm_build_94)
}

func (dm_build_96 *dm_build_2) Dm_build_95(dm_build_97 *DmStatement, dm_build_98 [][]interface{}) (*execRetInfo, error) {
	dm_build_99 := dm_build_554(dm_build_96, dm_build_97, dm_build_98)
	dm_build_100, dm_build_101 := dm_build_96.dm_build_45(dm_build_99)
	if dm_build_101 != nil {
		return nil, dm_build_101
	}
	return dm_build_100.(*execRetInfo), nil
}

func (dm_build_103 *dm_build_2) Dm_build_102(dm_build_104 *DmStatement, dm_build_105 [][]interface{}, dm_build_106 bool) (*execRetInfo, error) {
	var dm_build_107, dm_build_108 = 0, 0
	var dm_build_109 = len(dm_build_105)
	var dm_build_110 [][]interface{}
	var dm_build_111 = NewExceInfo()
	dm_build_111.updateCounts = make([]int64, dm_build_109)
	var dm_build_112 = false
	for dm_build_107 < dm_build_109 {
		for dm_build_108 = dm_build_107; dm_build_108 < dm_build_109; dm_build_108++ {
			paramData := dm_build_105[dm_build_108]
			bindData := make([]interface{}, dm_build_104.paramCount)
			dm_build_112 = false
			for icol := 0; icol < int(dm_build_104.paramCount); icol++ {
				if dm_build_104.bindParams[icol].ioType == IO_TYPE_OUT {
					continue
				}
				if dm_build_103.dm_build_247(bindData, paramData, icol) {
					dm_build_112 = true
					break
				}
			}

			if dm_build_112 {
				break
			}
			dm_build_110 = append(dm_build_110, bindData)
		}

		if dm_build_108 != dm_build_107 {
			tmpExecInfo, err := dm_build_103.Dm_build_95(dm_build_104, dm_build_110)
			if err != nil {
				return nil, err
			}
			dm_build_110 = dm_build_110[0:0]
			dm_build_111.union(tmpExecInfo, dm_build_107, dm_build_108-dm_build_107)
		}

		if dm_build_108 < dm_build_109 {
			tmpExecInfo, err := dm_build_103.Dm_build_121(dm_build_104, dm_build_105[dm_build_108], dm_build_106)
			if err != nil {
				return nil, err
			}

			dm_build_106 = true
			dm_build_111.union(tmpExecInfo, dm_build_108, 1)
		}

		dm_build_107 = dm_build_108 + 1
	}
	for _, i := range dm_build_111.updateCounts {
		if i > 0 {
			dm_build_111.updateCount += i
		}
	}
	return dm_build_111, nil
}

func (dm_build_114 *dm_build_2) dm_build_113(dm_build_115 *DmStatement, dm_build_116 []parameter) error {
	if !dm_build_115.prepared {
		retInfo, err := dm_build_114.Dm_build_73(dm_build_115, false, Dm_build_378)
		if err != nil {
			return nil
		}
		dm_build_115.serverParams = retInfo.serverParams
		dm_build_115.paramCount = int32(len(dm_build_115.serverParams))
		dm_build_115.prepared = true
	}

	dm_build_117 := dm_build_776(dm_build_114, dm_build_115, dm_build_115.bindParams)
	dm_build_118, err := dm_build_114.dm_build_45(dm_build_117)
	if err != nil {
		return nil
	}
	retInfo := dm_build_118.(*execRetInfo)
	if retInfo.serverParams != nil && len(retInfo.serverParams) > 0 {
		dm_build_115.serverParams = retInfo.serverParams
		dm_build_115.paramCount = int32(len(dm_build_115.serverParams))
	}
	dm_build_115.preExec = true
	return nil
}

func (dm_build_122 *dm_build_2) Dm_build_121(dm_build_123 *DmStatement, dm_build_124 []interface{}, dm_build_125 bool) (*execRetInfo, error) {

	var dm_build_126 = make([]interface{}, dm_build_123.paramCount)
	for icol := 0; icol < int(dm_build_123.paramCount); icol++ {
		if dm_build_123.bindParams[icol].ioType == IO_TYPE_OUT {
			continue
		}
		if dm_build_122.dm_build_247(dm_build_126, dm_build_124, icol) {

			if !dm_build_125 {
				dm_build_122.dm_build_113(dm_build_123, dm_build_123.bindParams)

				dm_build_125 = true
			}

			dm_build_122.dm_build_253(dm_build_123, dm_build_123.bindParams[icol], icol, dm_build_124[icol].(iOffRowBinder))
			dm_build_126[icol] = ParamDataEnum_OFF_ROW
		}
	}

	var dm_build_127 = make([][]interface{}, 1, 1)
	dm_build_127[0] = dm_build_126

	dm_build_128 := dm_build_554(dm_build_122, dm_build_123, dm_build_127)
	dm_build_129, dm_build_130 := dm_build_122.dm_build_45(dm_build_128)
	if dm_build_130 != nil {
		return nil, dm_build_130
	}
	return dm_build_129.(*execRetInfo), nil
}

func (dm_build_132 *dm_build_2) Dm_build_131(dm_build_133 *DmStatement, dm_build_134 int16) (*execRetInfo, error) {
	dm_build_135 := dm_build_763(dm_build_132, dm_build_133, dm_build_134)

	dm_build_136, dm_build_137 := dm_build_132.dm_build_45(dm_build_135)
	if dm_build_137 != nil {
		return nil, dm_build_137
	}
	return dm_build_136.(*execRetInfo), nil
}

func (dm_build_139 *dm_build_2) Dm_build_138(dm_build_140 *innerRows, dm_build_141 int64) (*execRetInfo, error) {
	dm_build_142 := dm_build_662(dm_build_139, dm_build_140, dm_build_141, INT64_MAX)
	dm_build_143, dm_build_144 := dm_build_139.dm_build_45(dm_build_142)
	if dm_build_144 != nil {
		return nil, dm_build_144
	}
	return dm_build_143.(*execRetInfo), nil
}

func (dm_build_146 *dm_build_2) Commit() error {
	dm_build_147 := dm_build_507(dm_build_146)
	_, dm_build_148 := dm_build_146.dm_build_45(dm_build_147)
	if dm_build_148 != nil {
		return dm_build_148
	}

	return nil
}

func (dm_build_150 *dm_build_2) Rollback() error {
	dm_build_151 := dm_build_825(dm_build_150)
	_, dm_build_152 := dm_build_150.dm_build_45(dm_build_151)
	if dm_build_152 != nil {
		return dm_build_152
	}

	return nil
}

func (dm_build_154 *dm_build_2) Dm_build_153(dm_build_155 *DmConnection) error {
	dm_build_156 := dm_build_830(dm_build_154, dm_build_155.IsoLevel)
	_, dm_build_157 := dm_build_154.dm_build_45(dm_build_156)
	if dm_build_157 != nil {
		return dm_build_157
	}

	return nil
}

func (dm_build_159 *dm_build_2) Dm_build_158(dm_build_160 *DmStatement, dm_build_161 string) error {
	dm_build_162 := dm_build_512(dm_build_159, dm_build_160, dm_build_161)
	_, dm_build_163 := dm_build_159.dm_build_45(dm_build_162)
	if dm_build_163 != nil {
		return dm_build_163
	}

	return nil
}

func (dm_build_165 *dm_build_2) Dm_build_164(dm_build_166 []uint32) ([]int64, error) {
	dm_build_167 := dm_build_932(dm_build_165, dm_build_166)
	dm_build_168, dm_build_169 := dm_build_165.dm_build_45(dm_build_167)
	if dm_build_169 != nil {
		return nil, dm_build_169
	}
	return dm_build_168.([]int64), nil
}

func (dm_build_171 *dm_build_2) Close() error {
	if dm_build_171.dm_build_13 {
		return nil
	}

	dm_build_172 := dm_build_171.dm_build_3.Close()
	if dm_build_172 != nil {
		return dm_build_172
	}

	dm_build_171.dm_build_6 = nil
	dm_build_171.dm_build_13 = true
	return nil
}

func (dm_build_174 *dm_build_2) dm_build_173(dm_build_175 *lob) (int64, error) {
	dm_build_176 := dm_build_695(dm_build_174, dm_build_175)
	dm_build_177, dm_build_178 := dm_build_174.dm_build_45(dm_build_176)
	if dm_build_178 != nil {
		return 0, dm_build_178
	}
	return dm_build_177.(int64), nil
}

func (dm_build_180 *dm_build_2) dm_build_179(dm_build_181 *lob, dm_build_182 int32, dm_build_183 int32) (*lobRetInfo, error) {
	dm_build_184 := dm_build_680(dm_build_180, dm_build_181, int(dm_build_182), int(dm_build_183))
	dm_build_185, dm_build_186 := dm_build_180.dm_build_45(dm_build_184)
	if dm_build_186 != nil {
		return nil, dm_build_186
	}
	return dm_build_185.(*lobRetInfo), nil
}

func (dm_build_188 *dm_build_2) dm_build_187(dm_build_189 *DmBlob, dm_build_190 int32, dm_build_191 int32) ([]byte, error) {
	var dm_build_192 = make([]byte, dm_build_191)
	var dm_build_193 int32 = 0
	var dm_build_194 int32 = 0
	var dm_build_195 *lobRetInfo
	var dm_build_196 []byte
	var dm_build_197 error
	for dm_build_193 < dm_build_191 {
		dm_build_194 = dm_build_191 - dm_build_193
		if dm_build_194 > Dm_build_412 {
			dm_build_194 = Dm_build_412
		}
		dm_build_195, dm_build_197 = dm_build_188.dm_build_179(&dm_build_189.lob, dm_build_190+dm_build_193, dm_build_194)
		if dm_build_197 != nil {
			return nil, dm_build_197
		}
		dm_build_196 = dm_build_195.data
		if dm_build_196 == nil || len(dm_build_196) == 0 {
			break
		}
		Dm_build_943.Dm_build_999(dm_build_192, int(dm_build_193), dm_build_196, 0, len(dm_build_196))
		dm_build_193 += int32(len(dm_build_196))
		if dm_build_189.readOver {
			break
		}
	}
	return dm_build_192, nil
}

func (dm_build_199 *dm_build_2) dm_build_198(dm_build_200 *DmClob, dm_build_201 int32, dm_build_202 int32) (string, error) {
	var dm_build_203 bytes.Buffer
	var dm_build_204 int32 = 0
	var dm_build_205 int32 = 0
	var dm_build_206 *lobRetInfo
	var dm_build_207 []byte
	var dm_build_208 string
	var dm_build_209 error
	for dm_build_204 < dm_build_202 {
		dm_build_205 = dm_build_202 - dm_build_204
		if dm_build_205 > Dm_build_412/2 {
			dm_build_205 = Dm_build_412 / 2
		}
		dm_build_206, dm_build_209 = dm_build_199.dm_build_179(&dm_build_200.lob, dm_build_201+dm_build_204, dm_build_205)
		if dm_build_209 != nil {
			return "", dm_build_209
		}
		dm_build_207 = dm_build_206.data
		if dm_build_207 == nil || len(dm_build_207) == 0 {
			break
		}
		dm_build_208 = Dm_build_943.Dm_build_1100(dm_build_207, 0, len(dm_build_207), dm_build_200.serverEncoding, dm_build_199.dm_build_6)

		dm_build_203.WriteString(dm_build_208)
		var strLen = dm_build_206.charLen
		if strLen == -1 {
			strLen = int64(utf8.RuneCountInString(dm_build_208))
		}
		dm_build_204 += int32(strLen)
		if dm_build_200.readOver {
			break
		}
	}
	return dm_build_203.String(), nil
}

func (dm_build_211 *dm_build_2) dm_build_210(dm_build_212 *DmClob, dm_build_213 int, dm_build_214 string, dm_build_215 string) (int, error) {
	var dm_build_216 = Dm_build_943.Dm_build_1159(dm_build_214, dm_build_215, dm_build_211.dm_build_6)
	var dm_build_217 = 0
	var dm_build_218 = len(dm_build_216)
	var dm_build_219 = 0
	var dm_build_220 = 0
	var dm_build_221 = 0
	var dm_build_222 = dm_build_218/Dm_build_411 + 1
	var dm_build_223 byte = 0
	var dm_build_224 byte = 0x01
	var dm_build_225 byte = 0x02
	for i := 0; i < dm_build_222; i++ {
		dm_build_223 = 0
		if i == 0 {
			dm_build_223 |= dm_build_224
		}
		if i == dm_build_222-1 {
			dm_build_223 |= dm_build_225
		}
		dm_build_221 = dm_build_218 - dm_build_220
		if dm_build_221 > Dm_build_411 {
			dm_build_221 = Dm_build_411
		}

		setLobData := dm_build_844(dm_build_211, &dm_build_212.lob, dm_build_223, dm_build_213, dm_build_216, dm_build_217, dm_build_221)
		ret, err := dm_build_211.dm_build_45(setLobData)
		if err != nil {
			return 0, err
		}
		tmp := ret.(int32)
		if err != nil {
			return -1, err
		}
		if tmp <= 0 {
			return dm_build_219, nil
		} else {
			dm_build_213 += int(tmp)
			dm_build_219 += int(tmp)
			dm_build_220 += dm_build_221
			dm_build_217 += dm_build_221
		}
	}
	return dm_build_219, nil
}

func (dm_build_227 *dm_build_2) dm_build_226(dm_build_228 *DmBlob, dm_build_229 int, dm_build_230 []byte) (int, error) {
	var dm_build_231 = 0
	var dm_build_232 = len(dm_build_230)
	var dm_build_233 = 0
	var dm_build_234 = 0
	var dm_build_235 = 0
	var dm_build_236 = dm_build_232/Dm_build_411 + 1
	var dm_build_237 byte = 0
	var dm_build_238 byte = 0x01
	var dm_build_239 byte = 0x02
	for i := 0; i < dm_build_236; i++ {
		dm_build_237 = 0
		if i == 0 {
			dm_build_237 |= dm_build_238
		}
		if i == dm_build_236-1 {
			dm_build_237 |= dm_build_239
		}
		dm_build_235 = dm_build_232 - dm_build_234
		if dm_build_235 > Dm_build_411 {
			dm_build_235 = Dm_build_411
		}

		setLobData := dm_build_844(dm_build_227, &dm_build_228.lob, dm_build_237, dm_build_229, dm_build_230, dm_build_231, dm_build_235)
		ret, err := dm_build_227.dm_build_45(setLobData)
		if err != nil {
			return 0, err
		}
		tmp := ret.(int32)
		if tmp <= 0 {
			return dm_build_233, nil
		} else {
			dm_build_229 += int(tmp)
			dm_build_233 += int(tmp)
			dm_build_234 += dm_build_235
			dm_build_231 += dm_build_235
		}
	}
	return dm_build_233, nil
}

func (dm_build_241 *dm_build_2) dm_build_240(dm_build_242 *lob, dm_build_243 int) (int64, error) {
	dm_build_244 := dm_build_706(dm_build_241, dm_build_242, dm_build_243)
	dm_build_245, dm_build_246 := dm_build_241.dm_build_45(dm_build_244)
	if dm_build_246 != nil {
		return dm_build_242.length, dm_build_246
	}
	return dm_build_245.(int64), nil
}

func (dm_build_248 *dm_build_2) dm_build_247(dm_build_249 []interface{}, dm_build_250 []interface{}, dm_build_251 int) bool {
	var dm_build_252 = false
	dm_build_249[dm_build_251] = dm_build_250[dm_build_251]

	if binder, ok := dm_build_250[dm_build_251].(iOffRowBinder); ok {
		dm_build_252 = true
		dm_build_249[dm_build_251] = make([]byte, 0)
		var lob lob
		if l, ok := binder.getObj().(DmBlob); ok {
			lob = l.lob
		} else if l, ok := binder.getObj().(DmClob); ok {
			lob = l.lob
		}
		if &lob != nil && lob.canOptimized(dm_build_248.dm_build_6) {
			dm_build_249[dm_build_251] = &lobCtl{lob.buildCtlData()}
			dm_build_252 = false
		}
	} else {
		dm_build_249[dm_build_251] = dm_build_250[dm_build_251]
	}
	return dm_build_252
}

func (dm_build_254 *dm_build_2) dm_build_253(dm_build_255 *DmStatement, dm_build_256 parameter, dm_build_257 int, dm_build_258 iOffRowBinder) error {
	var dm_build_259 = Dm_build_1228()
	dm_build_258.read(dm_build_259)
	var dm_build_260 = 0
	for !dm_build_258.isReadOver() || dm_build_259.Dm_build_1229() > 0 {
		if !dm_build_258.isReadOver() && dm_build_259.Dm_build_1229() < Dm_build_411 {
			dm_build_258.read(dm_build_259)
		}
		if dm_build_259.Dm_build_1229() > Dm_build_411 {
			dm_build_260 = Dm_build_411
		} else {
			dm_build_260 = dm_build_259.Dm_build_1229()
		}

		putData := dm_build_815(dm_build_254, dm_build_255, int16(dm_build_257), dm_build_259, int32(dm_build_260))
		_, err := dm_build_254.dm_build_45(putData)
		if err != nil {
			return err
		}
	}
	return nil
}

func (dm_build_262 *dm_build_2) dm_build_261() ([]byte, error) {
	var dm_build_263 error
	if dm_build_262.dm_build_10 == nil {
		if dm_build_262.dm_build_10, dm_build_263 = security.NewClientKeyPair(); dm_build_263 != nil {
			return nil, dm_build_263
		}
	}
	return security.Bn2Bytes(dm_build_262.dm_build_10.GetY(), security.DH_KEY_LENGTH), nil
}

func (dm_build_265 *dm_build_2) dm_build_264() (*security.DhKey, error) {
	var dm_build_266 error
	if dm_build_265.dm_build_10 == nil {
		if dm_build_265.dm_build_10, dm_build_266 = security.NewClientKeyPair(); dm_build_266 != nil {
			return nil, dm_build_266
		}
	}
	return dm_build_265.dm_build_10, nil
}

func (dm_build_268 *dm_build_2) dm_build_267(dm_build_269 int, dm_build_270 []byte, dm_build_271 string, dm_build_272 int) (dm_build_273 error) {
	if dm_build_269 > 0 && dm_build_269 < security.MIN_EXTERNAL_CIPHER_ID && dm_build_270 != nil {
		dm_build_268.dm_build_7, dm_build_273 = security.NewSymmCipher(dm_build_269, dm_build_270)
	} else if dm_build_269 >= security.MIN_EXTERNAL_CIPHER_ID {
		if dm_build_268.dm_build_7, dm_build_273 = security.NewThirdPartCipher(dm_build_269, dm_build_270, dm_build_271, dm_build_272); dm_build_273 != nil {
			dm_build_273 = THIRD_PART_CIPHER_INIT_FAILED.addDetailln(dm_build_273.Error()).throw()
		}
	}
	return
}

func (dm_build_275 *dm_build_2) dm_build_274(dm_build_276 bool, dm_build_277 bool) (dm_build_278 error) {
	if dm_build_275.dm_build_4, dm_build_278 = security.NewTLSFromTCP(dm_build_275.dm_build_3, dm_build_275.dm_build_6.dmConnector.sslCertPath, dm_build_275.dm_build_6.dmConnector.sslKeyPath, dm_build_275.dm_build_6.dmConnector.user, dm_build_277); dm_build_278 != nil {
		return
	}
	if !dm_build_276 {
		dm_build_275.dm_build_4 = nil
	}
	return
}

func (dm_build_280 *dm_build_2) dm_build_279(dm_build_281 dm_build_419) bool {
	return dm_build_281.dm_build_434() != Dm_build_325 && (dm_build_280.dm_build_6.sslEncrypt == 1 || dm_build_280.dm_build_6.sslEncrypt == 4)
}
