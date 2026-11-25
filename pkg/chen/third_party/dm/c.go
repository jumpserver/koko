/*
 * Copyright (c) 2000-2018, 达梦数据库有限公司.
 * All rights reserved.
 */
package dm

import (
	"io"
	"math"
)

type Dm_build_1302 struct {
	dm_build_1303 []byte
	dm_build_1304 int
}

func Dm_build_1305(dm_build_1306 int) *Dm_build_1302 {
	return &Dm_build_1302{make([]byte, 0, dm_build_1306), 0}
}

func Dm_build_1307(dm_build_1308 []byte) *Dm_build_1302 {
	return &Dm_build_1302{dm_build_1308, 0}
}

func (dm_build_1310 *Dm_build_1302) dm_build_1309(dm_build_1311 int) *Dm_build_1302 {

	dm_build_1312 := len(dm_build_1310.dm_build_1303)
	dm_build_1313 := cap(dm_build_1310.dm_build_1303)

	if dm_build_1312+dm_build_1311 <= dm_build_1313 {
		dm_build_1310.dm_build_1303 = dm_build_1310.dm_build_1303[:dm_build_1312+dm_build_1311]
	} else {

		var calCap = int64(math.Max(float64(2*dm_build_1313), float64(dm_build_1311+dm_build_1312)))

		nbuf := make([]byte, dm_build_1311+dm_build_1312, calCap)
		copy(nbuf, dm_build_1310.dm_build_1303)
		dm_build_1310.dm_build_1303 = nbuf
	}

	return dm_build_1310
}

func (dm_build_1315 *Dm_build_1302) Dm_build_1314() int {
	return len(dm_build_1315.dm_build_1303)
}

func (dm_build_1317 *Dm_build_1302) Dm_build_1316(dm_build_1318 int) *Dm_build_1302 {
	for i := dm_build_1318; i < len(dm_build_1317.dm_build_1303); i++ {
		dm_build_1317.dm_build_1303[i] = 0
	}
	dm_build_1317.dm_build_1303 = dm_build_1317.dm_build_1303[:dm_build_1318]
	return dm_build_1317
}

func (dm_build_1320 *Dm_build_1302) Dm_build_1319(dm_build_1321 int) *Dm_build_1302 {
	dm_build_1320.dm_build_1304 = dm_build_1321
	return dm_build_1320
}

func (dm_build_1323 *Dm_build_1302) Dm_build_1322() int {
	return dm_build_1323.dm_build_1304
}

func (dm_build_1325 *Dm_build_1302) Dm_build_1324(dm_build_1326 bool) int {
	return len(dm_build_1325.dm_build_1303) - dm_build_1325.dm_build_1304
}

func (dm_build_1328 *Dm_build_1302) Dm_build_1327(dm_build_1329 int, dm_build_1330 bool, dm_build_1331 bool) *Dm_build_1302 {

	if dm_build_1330 {
		if dm_build_1331 {
			dm_build_1328.dm_build_1309(dm_build_1329)
		} else {
			dm_build_1328.dm_build_1303 = dm_build_1328.dm_build_1303[:len(dm_build_1328.dm_build_1303)-dm_build_1329]
		}
	} else {
		if dm_build_1331 {
			dm_build_1328.dm_build_1304 += dm_build_1329
		} else {
			dm_build_1328.dm_build_1304 -= dm_build_1329
		}
	}

	return dm_build_1328
}

func (dm_build_1333 *Dm_build_1302) Dm_build_1332(dm_build_1334 io.Reader, dm_build_1335 int) (int, error) {
	dm_build_1336 := len(dm_build_1333.dm_build_1303)
	dm_build_1333.dm_build_1309(dm_build_1335)
	dm_build_1337 := 0
	for dm_build_1335 > 0 {
		n, err := dm_build_1334.Read(dm_build_1333.dm_build_1303[dm_build_1336+dm_build_1337:])
		if n > 0 && err == io.EOF {
			dm_build_1337 += n
			dm_build_1333.dm_build_1303 = dm_build_1333.dm_build_1303[:dm_build_1336+dm_build_1337]
			return dm_build_1337, nil
		} else if n > 0 && err == nil {
			dm_build_1335 -= n
			dm_build_1337 += n
		} else if n == 0 && err != nil {
			return -1, ECGO_COMMUNITION_ERROR.addDetailln(err.Error()).throw()
		}
	}

	return dm_build_1337, nil
}

func (dm_build_1339 *Dm_build_1302) Dm_build_1338(dm_build_1340 io.Writer) (*Dm_build_1302, error) {
	if _, err := dm_build_1340.Write(dm_build_1339.dm_build_1303); err != nil {
		return nil, ECGO_COMMUNITION_ERROR.addDetailln(err.Error()).throw()
	}
	return dm_build_1339, nil
}

func (dm_build_1342 *Dm_build_1302) Dm_build_1341(dm_build_1343 bool) int {
	dm_build_1344 := len(dm_build_1342.dm_build_1303)
	dm_build_1342.dm_build_1309(1)

	if dm_build_1343 {
		return copy(dm_build_1342.dm_build_1303[dm_build_1344:], []byte{1})
	} else {
		return copy(dm_build_1342.dm_build_1303[dm_build_1344:], []byte{0})
	}
}

func (dm_build_1346 *Dm_build_1302) Dm_build_1345(dm_build_1347 byte) int {
	dm_build_1348 := len(dm_build_1346.dm_build_1303)
	dm_build_1346.dm_build_1309(1)

	return copy(dm_build_1346.dm_build_1303[dm_build_1348:], Dm_build_943.Dm_build_1121(dm_build_1347))
}

func (dm_build_1350 *Dm_build_1302) Dm_build_1349(dm_build_1351 int8) int {
	dm_build_1352 := len(dm_build_1350.dm_build_1303)
	dm_build_1350.dm_build_1309(1)

	return copy(dm_build_1350.dm_build_1303[dm_build_1352:], Dm_build_943.Dm_build_1124(dm_build_1351))
}

func (dm_build_1354 *Dm_build_1302) Dm_build_1353(dm_build_1355 int16) int {
	dm_build_1356 := len(dm_build_1354.dm_build_1303)
	dm_build_1354.dm_build_1309(2)

	return copy(dm_build_1354.dm_build_1303[dm_build_1356:], Dm_build_943.Dm_build_1127(dm_build_1355))
}

func (dm_build_1358 *Dm_build_1302) Dm_build_1357(dm_build_1359 int32) int {
	dm_build_1360 := len(dm_build_1358.dm_build_1303)
	dm_build_1358.dm_build_1309(4)

	return copy(dm_build_1358.dm_build_1303[dm_build_1360:], Dm_build_943.Dm_build_1130(dm_build_1359))
}

func (dm_build_1362 *Dm_build_1302) Dm_build_1361(dm_build_1363 uint8) int {
	dm_build_1364 := len(dm_build_1362.dm_build_1303)
	dm_build_1362.dm_build_1309(1)

	return copy(dm_build_1362.dm_build_1303[dm_build_1364:], Dm_build_943.Dm_build_1142(dm_build_1363))
}

func (dm_build_1366 *Dm_build_1302) Dm_build_1365(dm_build_1367 uint16) int {
	dm_build_1368 := len(dm_build_1366.dm_build_1303)
	dm_build_1366.dm_build_1309(2)

	return copy(dm_build_1366.dm_build_1303[dm_build_1368:], Dm_build_943.Dm_build_1145(dm_build_1367))
}

func (dm_build_1370 *Dm_build_1302) Dm_build_1369(dm_build_1371 uint32) int {
	dm_build_1372 := len(dm_build_1370.dm_build_1303)
	dm_build_1370.dm_build_1309(4)

	return copy(dm_build_1370.dm_build_1303[dm_build_1372:], Dm_build_943.Dm_build_1148(dm_build_1371))
}

func (dm_build_1374 *Dm_build_1302) Dm_build_1373(dm_build_1375 uint64) int {
	dm_build_1376 := len(dm_build_1374.dm_build_1303)
	dm_build_1374.dm_build_1309(8)

	return copy(dm_build_1374.dm_build_1303[dm_build_1376:], Dm_build_943.Dm_build_1151(dm_build_1375))
}

func (dm_build_1378 *Dm_build_1302) Dm_build_1377(dm_build_1379 float32) int {
	dm_build_1380 := len(dm_build_1378.dm_build_1303)
	dm_build_1378.dm_build_1309(4)

	return copy(dm_build_1378.dm_build_1303[dm_build_1380:], Dm_build_943.Dm_build_1148(math.Float32bits(dm_build_1379)))
}

func (dm_build_1382 *Dm_build_1302) Dm_build_1381(dm_build_1383 float64) int {
	dm_build_1384 := len(dm_build_1382.dm_build_1303)
	dm_build_1382.dm_build_1309(8)

	return copy(dm_build_1382.dm_build_1303[dm_build_1384:], Dm_build_943.Dm_build_1151(math.Float64bits(dm_build_1383)))
}

func (dm_build_1386 *Dm_build_1302) Dm_build_1385(dm_build_1387 []byte) int {
	dm_build_1388 := len(dm_build_1386.dm_build_1303)
	dm_build_1386.dm_build_1309(len(dm_build_1387))
	return copy(dm_build_1386.dm_build_1303[dm_build_1388:], dm_build_1387)
}

func (dm_build_1390 *Dm_build_1302) Dm_build_1389(dm_build_1391 []byte) int {
	return dm_build_1390.Dm_build_1357(int32(len(dm_build_1391))) + dm_build_1390.Dm_build_1385(dm_build_1391)
}

func (dm_build_1393 *Dm_build_1302) Dm_build_1392(dm_build_1394 []byte) int {
	return dm_build_1393.Dm_build_1361(uint8(len(dm_build_1394))) + dm_build_1393.Dm_build_1385(dm_build_1394)
}

func (dm_build_1396 *Dm_build_1302) Dm_build_1395(dm_build_1397 []byte) int {
	return dm_build_1396.Dm_build_1365(uint16(len(dm_build_1397))) + dm_build_1396.Dm_build_1385(dm_build_1397)
}

func (dm_build_1399 *Dm_build_1302) Dm_build_1398(dm_build_1400 []byte) int {
	return dm_build_1399.Dm_build_1385(dm_build_1400) + dm_build_1399.Dm_build_1345(0)
}

func (dm_build_1402 *Dm_build_1302) Dm_build_1401(dm_build_1403 string, dm_build_1404 string, dm_build_1405 *DmConnection) int {
	dm_build_1406 := Dm_build_943.Dm_build_1159(dm_build_1403, dm_build_1404, dm_build_1405)
	return dm_build_1402.Dm_build_1389(dm_build_1406)
}

func (dm_build_1408 *Dm_build_1302) Dm_build_1407(dm_build_1409 string, dm_build_1410 string, dm_build_1411 *DmConnection) int {
	dm_build_1412 := Dm_build_943.Dm_build_1159(dm_build_1409, dm_build_1410, dm_build_1411)
	return dm_build_1408.Dm_build_1392(dm_build_1412)
}

func (dm_build_1414 *Dm_build_1302) Dm_build_1413(dm_build_1415 string, dm_build_1416 string, dm_build_1417 *DmConnection) int {
	dm_build_1418 := Dm_build_943.Dm_build_1159(dm_build_1415, dm_build_1416, dm_build_1417)
	return dm_build_1414.Dm_build_1395(dm_build_1418)
}

func (dm_build_1420 *Dm_build_1302) Dm_build_1419(dm_build_1421 string, dm_build_1422 string, dm_build_1423 *DmConnection) int {
	dm_build_1424 := Dm_build_943.Dm_build_1159(dm_build_1421, dm_build_1422, dm_build_1423)
	return dm_build_1420.Dm_build_1398(dm_build_1424)
}

func (dm_build_1426 *Dm_build_1302) Dm_build_1425() byte {
	dm_build_1427 := Dm_build_943.Dm_build_1036(dm_build_1426.dm_build_1303, dm_build_1426.dm_build_1304)
	dm_build_1426.dm_build_1304++
	return dm_build_1427
}

func (dm_build_1429 *Dm_build_1302) Dm_build_1428() int16 {
	dm_build_1430 := Dm_build_943.Dm_build_1040(dm_build_1429.dm_build_1303, dm_build_1429.dm_build_1304)
	dm_build_1429.dm_build_1304 += 2
	return dm_build_1430
}

func (dm_build_1432 *Dm_build_1302) Dm_build_1431() int32 {
	dm_build_1433 := Dm_build_943.Dm_build_1045(dm_build_1432.dm_build_1303, dm_build_1432.dm_build_1304)
	dm_build_1432.dm_build_1304 += 4
	return dm_build_1433
}

func (dm_build_1435 *Dm_build_1302) Dm_build_1434() int64 {
	dm_build_1436 := Dm_build_943.Dm_build_1050(dm_build_1435.dm_build_1303, dm_build_1435.dm_build_1304)
	dm_build_1435.dm_build_1304 += 8
	return dm_build_1436
}

func (dm_build_1438 *Dm_build_1302) Dm_build_1437() float32 {
	dm_build_1439 := Dm_build_943.Dm_build_1055(dm_build_1438.dm_build_1303, dm_build_1438.dm_build_1304)
	dm_build_1438.dm_build_1304 += 4
	return dm_build_1439
}

func (dm_build_1441 *Dm_build_1302) Dm_build_1440() float64 {
	dm_build_1442 := Dm_build_943.Dm_build_1059(dm_build_1441.dm_build_1303, dm_build_1441.dm_build_1304)
	dm_build_1441.dm_build_1304 += 8
	return dm_build_1442
}

func (dm_build_1444 *Dm_build_1302) Dm_build_1443() uint8 {
	dm_build_1445 := Dm_build_943.Dm_build_1063(dm_build_1444.dm_build_1303, dm_build_1444.dm_build_1304)
	dm_build_1444.dm_build_1304 += 1
	return dm_build_1445
}

func (dm_build_1447 *Dm_build_1302) Dm_build_1446() uint16 {
	dm_build_1448 := Dm_build_943.Dm_build_1067(dm_build_1447.dm_build_1303, dm_build_1447.dm_build_1304)
	dm_build_1447.dm_build_1304 += 2
	return dm_build_1448
}

func (dm_build_1450 *Dm_build_1302) Dm_build_1449() uint32 {
	dm_build_1451 := Dm_build_943.Dm_build_1072(dm_build_1450.dm_build_1303, dm_build_1450.dm_build_1304)
	dm_build_1450.dm_build_1304 += 4
	return dm_build_1451
}

func (dm_build_1453 *Dm_build_1302) Dm_build_1452(dm_build_1454 int) []byte {
	dm_build_1455 := Dm_build_943.Dm_build_1094(dm_build_1453.dm_build_1303, dm_build_1453.dm_build_1304, dm_build_1454)
	dm_build_1453.dm_build_1304 += dm_build_1454
	return dm_build_1455
}

func (dm_build_1457 *Dm_build_1302) Dm_build_1456() []byte {
	return dm_build_1457.Dm_build_1452(int(dm_build_1457.Dm_build_1431()))
}

func (dm_build_1459 *Dm_build_1302) Dm_build_1458() []byte {
	return dm_build_1459.Dm_build_1452(int(dm_build_1459.Dm_build_1425()))
}

func (dm_build_1461 *Dm_build_1302) Dm_build_1460() []byte {
	return dm_build_1461.Dm_build_1452(int(dm_build_1461.Dm_build_1428()))
}

func (dm_build_1463 *Dm_build_1302) Dm_build_1462(dm_build_1464 int) []byte {
	return dm_build_1463.Dm_build_1452(dm_build_1464)
}

func (dm_build_1466 *Dm_build_1302) Dm_build_1465() []byte {
	dm_build_1467 := 0
	for dm_build_1466.Dm_build_1425() != 0 {
		dm_build_1467++
	}
	dm_build_1466.Dm_build_1327(dm_build_1467, false, false)
	return dm_build_1466.Dm_build_1452(dm_build_1467)
}

func (dm_build_1469 *Dm_build_1302) Dm_build_1468(dm_build_1470 int, dm_build_1471 string, dm_build_1472 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1469.Dm_build_1452(dm_build_1470), dm_build_1471, dm_build_1472)
}

func (dm_build_1474 *Dm_build_1302) Dm_build_1473(dm_build_1475 string, dm_build_1476 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1474.Dm_build_1456(), dm_build_1475, dm_build_1476)
}

func (dm_build_1478 *Dm_build_1302) Dm_build_1477(dm_build_1479 string, dm_build_1480 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1478.Dm_build_1458(), dm_build_1479, dm_build_1480)
}

func (dm_build_1482 *Dm_build_1302) Dm_build_1481(dm_build_1483 string, dm_build_1484 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1482.Dm_build_1460(), dm_build_1483, dm_build_1484)
}

func (dm_build_1486 *Dm_build_1302) Dm_build_1485(dm_build_1487 string, dm_build_1488 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1486.Dm_build_1465(), dm_build_1487, dm_build_1488)
}

func (dm_build_1490 *Dm_build_1302) Dm_build_1489(dm_build_1491 int, dm_build_1492 byte) int {
	return dm_build_1490.Dm_build_1525(dm_build_1491, Dm_build_943.Dm_build_1121(dm_build_1492))
}

func (dm_build_1494 *Dm_build_1302) Dm_build_1493(dm_build_1495 int, dm_build_1496 int16) int {
	return dm_build_1494.Dm_build_1525(dm_build_1495, Dm_build_943.Dm_build_1127(dm_build_1496))
}

func (dm_build_1498 *Dm_build_1302) Dm_build_1497(dm_build_1499 int, dm_build_1500 int32) int {
	return dm_build_1498.Dm_build_1525(dm_build_1499, Dm_build_943.Dm_build_1130(dm_build_1500))
}

func (dm_build_1502 *Dm_build_1302) Dm_build_1501(dm_build_1503 int, dm_build_1504 int64) int {
	return dm_build_1502.Dm_build_1525(dm_build_1503, Dm_build_943.Dm_build_1133(dm_build_1504))
}

func (dm_build_1506 *Dm_build_1302) Dm_build_1505(dm_build_1507 int, dm_build_1508 float32) int {
	return dm_build_1506.Dm_build_1525(dm_build_1507, Dm_build_943.Dm_build_1136(dm_build_1508))
}

func (dm_build_1510 *Dm_build_1302) Dm_build_1509(dm_build_1511 int, dm_build_1512 float64) int {
	return dm_build_1510.Dm_build_1525(dm_build_1511, Dm_build_943.Dm_build_1139(dm_build_1512))
}

func (dm_build_1514 *Dm_build_1302) Dm_build_1513(dm_build_1515 int, dm_build_1516 uint8) int {
	return dm_build_1514.Dm_build_1525(dm_build_1515, Dm_build_943.Dm_build_1142(dm_build_1516))
}

func (dm_build_1518 *Dm_build_1302) Dm_build_1517(dm_build_1519 int, dm_build_1520 uint16) int {
	return dm_build_1518.Dm_build_1525(dm_build_1519, Dm_build_943.Dm_build_1145(dm_build_1520))
}

func (dm_build_1522 *Dm_build_1302) Dm_build_1521(dm_build_1523 int, dm_build_1524 uint32) int {
	return dm_build_1522.Dm_build_1525(dm_build_1523, Dm_build_943.Dm_build_1148(dm_build_1524))
}

func (dm_build_1526 *Dm_build_1302) Dm_build_1525(dm_build_1527 int, dm_build_1528 []byte) int {
	return copy(dm_build_1526.dm_build_1303[dm_build_1527:], dm_build_1528)
}

func (dm_build_1530 *Dm_build_1302) Dm_build_1529(dm_build_1531 int, dm_build_1532 []byte) int {
	return dm_build_1530.Dm_build_1497(dm_build_1531, int32(len(dm_build_1532))) + dm_build_1530.Dm_build_1525(dm_build_1531+4, dm_build_1532)
}

func (dm_build_1534 *Dm_build_1302) Dm_build_1533(dm_build_1535 int, dm_build_1536 []byte) int {
	return dm_build_1534.Dm_build_1489(dm_build_1535, byte(len(dm_build_1536))) + dm_build_1534.Dm_build_1525(dm_build_1535+1, dm_build_1536)
}

func (dm_build_1538 *Dm_build_1302) Dm_build_1537(dm_build_1539 int, dm_build_1540 []byte) int {
	return dm_build_1538.Dm_build_1493(dm_build_1539, int16(len(dm_build_1540))) + dm_build_1538.Dm_build_1525(dm_build_1539+2, dm_build_1540)
}

func (dm_build_1542 *Dm_build_1302) Dm_build_1541(dm_build_1543 int, dm_build_1544 []byte) int {
	return dm_build_1542.Dm_build_1525(dm_build_1543, dm_build_1544) + dm_build_1542.Dm_build_1489(dm_build_1543+len(dm_build_1544), 0)
}

func (dm_build_1546 *Dm_build_1302) Dm_build_1545(dm_build_1547 int, dm_build_1548 string, dm_build_1549 string, dm_build_1550 *DmConnection) int {
	return dm_build_1546.Dm_build_1529(dm_build_1547, Dm_build_943.Dm_build_1159(dm_build_1548, dm_build_1549, dm_build_1550))
}

func (dm_build_1552 *Dm_build_1302) Dm_build_1551(dm_build_1553 int, dm_build_1554 string, dm_build_1555 string, dm_build_1556 *DmConnection) int {
	return dm_build_1552.Dm_build_1533(dm_build_1553, Dm_build_943.Dm_build_1159(dm_build_1554, dm_build_1555, dm_build_1556))
}

func (dm_build_1558 *Dm_build_1302) Dm_build_1557(dm_build_1559 int, dm_build_1560 string, dm_build_1561 string, dm_build_1562 *DmConnection) int {
	return dm_build_1558.Dm_build_1537(dm_build_1559, Dm_build_943.Dm_build_1159(dm_build_1560, dm_build_1561, dm_build_1562))
}

func (dm_build_1564 *Dm_build_1302) Dm_build_1563(dm_build_1565 int, dm_build_1566 string, dm_build_1567 string, dm_build_1568 *DmConnection) int {
	return dm_build_1564.Dm_build_1541(dm_build_1565, Dm_build_943.Dm_build_1159(dm_build_1566, dm_build_1567, dm_build_1568))
}

func (dm_build_1570 *Dm_build_1302) Dm_build_1569(dm_build_1571 int) byte {
	return Dm_build_943.Dm_build_1164(dm_build_1570.Dm_build_1596(dm_build_1571, 1))
}

func (dm_build_1573 *Dm_build_1302) Dm_build_1572(dm_build_1574 int) int16 {
	return Dm_build_943.Dm_build_1167(dm_build_1573.Dm_build_1596(dm_build_1574, 2))
}

func (dm_build_1576 *Dm_build_1302) Dm_build_1575(dm_build_1577 int) int32 {
	return Dm_build_943.Dm_build_1170(dm_build_1576.Dm_build_1596(dm_build_1577, 4))
}

func (dm_build_1579 *Dm_build_1302) Dm_build_1578(dm_build_1580 int) int64 {
	return Dm_build_943.Dm_build_1173(dm_build_1579.Dm_build_1596(dm_build_1580, 8))
}

func (dm_build_1582 *Dm_build_1302) Dm_build_1581(dm_build_1583 int) float32 {
	return Dm_build_943.Dm_build_1176(dm_build_1582.Dm_build_1596(dm_build_1583, 4))
}

func (dm_build_1585 *Dm_build_1302) Dm_build_1584(dm_build_1586 int) float64 {
	return Dm_build_943.Dm_build_1179(dm_build_1585.Dm_build_1596(dm_build_1586, 8))
}

func (dm_build_1588 *Dm_build_1302) Dm_build_1587(dm_build_1589 int) uint8 {
	return Dm_build_943.Dm_build_1182(dm_build_1588.Dm_build_1596(dm_build_1589, 1))
}

func (dm_build_1591 *Dm_build_1302) Dm_build_1590(dm_build_1592 int) uint16 {
	return Dm_build_943.Dm_build_1185(dm_build_1591.Dm_build_1596(dm_build_1592, 2))
}

func (dm_build_1594 *Dm_build_1302) Dm_build_1593(dm_build_1595 int) uint32 {
	return Dm_build_943.Dm_build_1188(dm_build_1594.Dm_build_1596(dm_build_1595, 4))
}

func (dm_build_1597 *Dm_build_1302) Dm_build_1596(dm_build_1598 int, dm_build_1599 int) []byte {
	return dm_build_1597.dm_build_1303[dm_build_1598 : dm_build_1598+dm_build_1599]
}

func (dm_build_1601 *Dm_build_1302) Dm_build_1600(dm_build_1602 int) []byte {
	dm_build_1603 := dm_build_1601.Dm_build_1575(dm_build_1602)
	return dm_build_1601.Dm_build_1596(dm_build_1602+4, int(dm_build_1603))
}

func (dm_build_1605 *Dm_build_1302) Dm_build_1604(dm_build_1606 int) []byte {
	dm_build_1607 := dm_build_1605.Dm_build_1569(dm_build_1606)
	return dm_build_1605.Dm_build_1596(dm_build_1606+1, int(dm_build_1607))
}

func (dm_build_1609 *Dm_build_1302) Dm_build_1608(dm_build_1610 int) []byte {
	dm_build_1611 := dm_build_1609.Dm_build_1572(dm_build_1610)
	return dm_build_1609.Dm_build_1596(dm_build_1610+2, int(dm_build_1611))
}

func (dm_build_1613 *Dm_build_1302) Dm_build_1612(dm_build_1614 int) []byte {
	dm_build_1615 := 0
	for dm_build_1613.Dm_build_1569(dm_build_1614) != 0 {
		dm_build_1614++
		dm_build_1615++
	}

	return dm_build_1613.Dm_build_1596(dm_build_1614-dm_build_1615, int(dm_build_1615))
}

func (dm_build_1617 *Dm_build_1302) Dm_build_1616(dm_build_1618 int, dm_build_1619 string, dm_build_1620 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1617.Dm_build_1600(dm_build_1618), dm_build_1619, dm_build_1620)
}

func (dm_build_1622 *Dm_build_1302) Dm_build_1621(dm_build_1623 int, dm_build_1624 string, dm_build_1625 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1622.Dm_build_1604(dm_build_1623), dm_build_1624, dm_build_1625)
}

func (dm_build_1627 *Dm_build_1302) Dm_build_1626(dm_build_1628 int, dm_build_1629 string, dm_build_1630 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1627.Dm_build_1608(dm_build_1628), dm_build_1629, dm_build_1630)
}

func (dm_build_1632 *Dm_build_1302) Dm_build_1631(dm_build_1633 int, dm_build_1634 string, dm_build_1635 *DmConnection) string {
	return Dm_build_943.Dm_build_1195(dm_build_1632.Dm_build_1612(dm_build_1633), dm_build_1634, dm_build_1635)
}
