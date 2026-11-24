/*
 * Copyright (c) 2000-2018, 达梦数据库有限公司.
 * All rights reserved.
 */
package dm

import (
	"container/list"
	"io"
)

type Dm_build_1224 struct {
	dm_build_1225 *list.List
	dm_build_1226 *dm_build_1278
	dm_build_1227 int
}

func Dm_build_1228() *Dm_build_1224 {
	return &Dm_build_1224{
		dm_build_1225: list.New(),
		dm_build_1227: 0,
	}
}

func (dm_build_1230 *Dm_build_1224) Dm_build_1229() int {
	return dm_build_1230.dm_build_1227
}

func (dm_build_1232 *Dm_build_1224) Dm_build_1231(dm_build_1233 *Dm_build_1302, dm_build_1234 int) int {
	var dm_build_1235 = 0
	var dm_build_1236 = 0
	for dm_build_1235 < dm_build_1234 && dm_build_1232.dm_build_1226 != nil {
		dm_build_1236 = dm_build_1232.dm_build_1226.dm_build_1286(dm_build_1233, dm_build_1234-dm_build_1235)
		if dm_build_1232.dm_build_1226.dm_build_1281 == 0 {
			dm_build_1232.dm_build_1268()
		}
		dm_build_1235 += dm_build_1236
		dm_build_1232.dm_build_1227 -= dm_build_1236
	}
	return dm_build_1235
}

func (dm_build_1238 *Dm_build_1224) Dm_build_1237(dm_build_1239 []byte, dm_build_1240 int, dm_build_1241 int) int {
	var dm_build_1242 = 0
	var dm_build_1243 = 0
	for dm_build_1242 < dm_build_1241 && dm_build_1238.dm_build_1226 != nil {
		dm_build_1243 = dm_build_1238.dm_build_1226.dm_build_1290(dm_build_1239, dm_build_1240, dm_build_1241-dm_build_1242)
		if dm_build_1238.dm_build_1226.dm_build_1281 == 0 {
			dm_build_1238.dm_build_1268()
		}
		dm_build_1242 += dm_build_1243
		dm_build_1238.dm_build_1227 -= dm_build_1243
		dm_build_1240 += dm_build_1243
	}
	return dm_build_1242
}

func (dm_build_1245 *Dm_build_1224) Dm_build_1244(dm_build_1246 io.Writer, dm_build_1247 int) int {
	var dm_build_1248 = 0
	var dm_build_1249 = 0
	for dm_build_1248 < dm_build_1247 && dm_build_1245.dm_build_1226 != nil {
		dm_build_1249 = dm_build_1245.dm_build_1226.dm_build_1295(dm_build_1246, dm_build_1247-dm_build_1248)
		if dm_build_1245.dm_build_1226.dm_build_1281 == 0 {
			dm_build_1245.dm_build_1268()
		}
		dm_build_1248 += dm_build_1249
		dm_build_1245.dm_build_1227 -= dm_build_1249
	}
	return dm_build_1248
}

func (dm_build_1251 *Dm_build_1224) Dm_build_1250(dm_build_1252 []byte, dm_build_1253 int, dm_build_1254 int) {
	if dm_build_1254 == 0 {
		return
	}
	var dm_build_1255 = dm_build_1282(dm_build_1252, dm_build_1253, dm_build_1254)
	if dm_build_1251.dm_build_1226 == nil {
		dm_build_1251.dm_build_1226 = dm_build_1255
	} else {
		dm_build_1251.dm_build_1225.PushBack(dm_build_1255)
	}
	dm_build_1251.dm_build_1227 += dm_build_1254
}

func (dm_build_1257 *Dm_build_1224) dm_build_1256(dm_build_1258 int) byte {
	var dm_build_1259 = dm_build_1258
	var dm_build_1260 = dm_build_1257.dm_build_1226
	for dm_build_1259 > 0 && dm_build_1260 != nil {
		if dm_build_1260.dm_build_1281 == 0 {
			continue
		}
		if dm_build_1259 > dm_build_1260.dm_build_1281-1 {
			dm_build_1259 -= dm_build_1260.dm_build_1281
			dm_build_1260 = dm_build_1257.dm_build_1225.Front().Value.(*dm_build_1278)
		} else {
			break
		}
	}
	return dm_build_1260.dm_build_1299(dm_build_1259)
}
func (dm_build_1262 *Dm_build_1224) Dm_build_1261(dm_build_1263 *Dm_build_1224) {
	if dm_build_1263.dm_build_1227 == 0 {
		return
	}
	var dm_build_1264 = dm_build_1263.dm_build_1226
	for dm_build_1264 != nil {
		dm_build_1262.dm_build_1265(dm_build_1264)
		dm_build_1263.dm_build_1268()
		dm_build_1264 = dm_build_1263.dm_build_1226
	}
	dm_build_1263.dm_build_1227 = 0
}
func (dm_build_1266 *Dm_build_1224) dm_build_1265(dm_build_1267 *dm_build_1278) {
	if dm_build_1267.dm_build_1281 == 0 {
		return
	}
	if dm_build_1266.dm_build_1226 == nil {
		dm_build_1266.dm_build_1226 = dm_build_1267
	} else {
		dm_build_1266.dm_build_1225.PushBack(dm_build_1267)
	}
	dm_build_1266.dm_build_1227 += dm_build_1267.dm_build_1281
}

func (dm_build_1269 *Dm_build_1224) dm_build_1268() {
	var dm_build_1270 = dm_build_1269.dm_build_1225.Front()
	if dm_build_1270 == nil {
		dm_build_1269.dm_build_1226 = nil
	} else {
		dm_build_1269.dm_build_1226 = dm_build_1270.Value.(*dm_build_1278)
		dm_build_1269.dm_build_1225.Remove(dm_build_1270)
	}
}

func (dm_build_1272 *Dm_build_1224) Dm_build_1271() []byte {
	var dm_build_1273 = make([]byte, dm_build_1272.dm_build_1227)
	var dm_build_1274 = dm_build_1272.dm_build_1226
	var dm_build_1275 = 0
	var dm_build_1276 = len(dm_build_1273)
	var dm_build_1277 = 0
	for dm_build_1274 != nil {
		if dm_build_1274.dm_build_1281 > 0 {
			if dm_build_1276 > dm_build_1274.dm_build_1281 {
				dm_build_1277 = dm_build_1274.dm_build_1281
			} else {
				dm_build_1277 = dm_build_1276
			}
			copy(dm_build_1273[dm_build_1275:dm_build_1275+dm_build_1277], dm_build_1274.dm_build_1279[dm_build_1274.dm_build_1280:dm_build_1274.dm_build_1280+dm_build_1277])
			dm_build_1275 += dm_build_1277
			dm_build_1276 -= dm_build_1277
		}
		if dm_build_1272.dm_build_1225.Front() == nil {
			dm_build_1274 = nil
		} else {
			dm_build_1274 = dm_build_1272.dm_build_1225.Front().Value.(*dm_build_1278)
		}
	}
	return dm_build_1273
}

type dm_build_1278 struct {
	dm_build_1279 []byte
	dm_build_1280 int
	dm_build_1281 int
}

func dm_build_1282(dm_build_1283 []byte, dm_build_1284 int, dm_build_1285 int) *dm_build_1278 {
	return &dm_build_1278{
		dm_build_1283,
		dm_build_1284,
		dm_build_1285,
	}
}

func (dm_build_1287 *dm_build_1278) dm_build_1286(dm_build_1288 *Dm_build_1302, dm_build_1289 int) int {
	if dm_build_1287.dm_build_1281 <= dm_build_1289 {
		dm_build_1289 = dm_build_1287.dm_build_1281
	}
	dm_build_1288.Dm_build_1385(dm_build_1287.dm_build_1279[dm_build_1287.dm_build_1280 : dm_build_1287.dm_build_1280+dm_build_1289])
	dm_build_1287.dm_build_1280 += dm_build_1289
	dm_build_1287.dm_build_1281 -= dm_build_1289
	return dm_build_1289
}

func (dm_build_1291 *dm_build_1278) dm_build_1290(dm_build_1292 []byte, dm_build_1293 int, dm_build_1294 int) int {
	if dm_build_1291.dm_build_1281 <= dm_build_1294 {
		dm_build_1294 = dm_build_1291.dm_build_1281
	}
	copy(dm_build_1292[dm_build_1293:dm_build_1293+dm_build_1294], dm_build_1291.dm_build_1279[dm_build_1291.dm_build_1280:dm_build_1291.dm_build_1280+dm_build_1294])
	dm_build_1291.dm_build_1280 += dm_build_1294
	dm_build_1291.dm_build_1281 -= dm_build_1294
	return dm_build_1294
}

func (dm_build_1296 *dm_build_1278) dm_build_1295(dm_build_1297 io.Writer, dm_build_1298 int) int {
	if dm_build_1296.dm_build_1281 <= dm_build_1298 {
		dm_build_1298 = dm_build_1296.dm_build_1281
	}
	dm_build_1297.Write(dm_build_1296.dm_build_1279[dm_build_1296.dm_build_1280 : dm_build_1296.dm_build_1280+dm_build_1298])
	dm_build_1296.dm_build_1280 += dm_build_1298
	dm_build_1296.dm_build_1281 -= dm_build_1298
	return dm_build_1298
}
func (dm_build_1300 *dm_build_1278) dm_build_1299(dm_build_1301 int) byte {
	return dm_build_1300.dm_build_1279[dm_build_1300.dm_build_1280+dm_build_1301]
}
