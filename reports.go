package ctdx

import (
	"io"
	"fmt"
	"log"
	"bytes"
	"strconv"
	"strings"
	"io/ioutil"
	"encoding/binary"
	"github.com/kniren/gota/series"
	"github.com/kniren/gota/dataframe"
	"github.com/klauspost/compress/zip"

	gbytes "github.com/datochan/gcom/bytes"

	"github.com/datochan/gcom/utils"

	"github.com/datochan/ctdx/comm"
	"github.com/datochan/ctdx/packet"
)

// ######## 财报数据
/**
 * 财报数据解析
 */
func reportAnalyse(reportRawData []byte, code string, date int) dataframe.DataFrame {
	var newBuffer bytes.Buffer
	var reportHeader packet.ReportHeader
	var reportItem packet.ReportItem
	var reportData packet.ReportData
	var reportList []map[string]interface{}

	headerSize := utils.SizeStruct(packet.ReportHeader{})
	itemSize := utils.SizeStruct(packet.ReportItem{})
	priceDataSize := uint32(utils.SizeStruct(packet.ReportData{}))

	tmpBuffer := reportRawData[:headerSize]
	newBuffer.Write(tmpBuffer)
	binary.Read(&newBuffer, binary.LittleEndian, &reportHeader)
	newBuffer.Reset()

	for idx:=0; idx<int(reportHeader.MaxCount); idx++ {
		start := headerSize + idx * itemSize
		tmpBuffer := reportRawData[start:start+itemSize]
		newBuffer.Write(tmpBuffer)
		binary.Read(&newBuffer, binary.LittleEndian, &reportItem)
		newBuffer.Reset()

		stockCode := gbytes.BytesToString(reportItem.Code[:])

		if len(code) > 0 {
			// 获取指定股票的财报信息
			if 0 == strings.Compare(stockCode, code) {
				priceRawData := reportRawData[reportItem.Foa:reportItem.Foa +priceDataSize]

				newBuffer.Write(priceRawData)
				binary.Read(&newBuffer, binary.LittleEndian, &reportData)
				newBuffer.Reset()

				reportMaps := make(map[string]interface{})
				reportMaps["code"] = code
				reportMaps["date"] = date
				for idx:=0; idx<len(reportData.Prices); idx++ {
					priceItem := reportData.Prices[idx]
					if priceItem < -10000000000000.00 {
						reportMaps[strconv.Itoa(idx+1)] = float64(0.00)
					} else {
						reportMaps[strconv.Itoa(idx+1)] = float64(priceItem)
					}
				}
				reportList = append(reportList, reportMaps)
			}

		} else {
			// 不指定股票代码时获取所有的财报信息
			priceRawData := reportRawData[reportItem.Foa:reportItem.Foa +priceDataSize]

			newBuffer.Write(priceRawData)
			binary.Read(&newBuffer, binary.LittleEndian, &reportData)
			newBuffer.Reset()

			reportMaps := make(map[string]interface{})
			reportMaps["code"] = stockCode
			reportMaps["date"] = date

			for idx:=0; idx<len(reportData.Prices); idx++ {
				priceItem := reportData.Prices[idx]
				if priceItem < -10000000000000.00 {
					reportMaps[strconv.Itoa(idx+1)] = 0
				} else {
					reportMaps[strconv.Itoa(idx+1)] = float64(priceItem)
				}
			}
			reportList = append(reportList, reportMaps)
		}
	}

	elemTypes := make(map[string]series.Type)

	elemTypes["code"] = series.String
	elemTypes["date"] = series.Int

	for idx:=0; idx<len(reportData.Prices); idx++ {
		elemTypes[strconv.Itoa(idx+1)] = series.Float
	}

	return dataframe.LoadMaps(reportList, dataframe.WithTypes(elemTypes))
}

/**
 * 获取指定日期中某只股票或者所有股票的财报信息
 * :param date: yyyymmdd
 * :param code:
 * :return:
 */
func reportList(conf comm.IConfigure, code string, date int) dataframe.DataFrame {
	var newBuffer bytes.Buffer

	fileName := fmt.Sprintf("gpcw%d", date)
	filePath := fmt.Sprintf("%s%s%s.zip", conf.GetApp().DataPath, conf.GetTdx().Files.StockReport, fileName)
	exist, err := utils.FileExists(filePath)
	if nil != err || ! exist {
		return dataframe.DataFrame{Err:fmt.Errorf("指定的财报文件不存在, Err=%v", err)}
	}

	r, err := zip.OpenReader(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	for _, f := range r.File {
		if -1 == strings.LastIndex(f.Name, ".dat") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return dataframe.DataFrame{Err:fmt.Errorf("财报文件解压失败, Err=%v", err)}
		}

		_, err = io.CopyN(&newBuffer, rc, int64(f.UncompressedSize64))
		if err != nil {
			return dataframe.DataFrame{Err:fmt.Errorf("读取财报内容失败, Err=%v", err)}
		}

		rc.Close()
		break
	}

	return reportAnalyse(newBuffer.Bytes(), code, date)
}

/**
	获取财报信息
	:param code: 指定股票
	:param date: yyyymmdd 指定日期
	:return:
	* 什么参数不指定返回所有股票的最后一季财报信息。
	* 仅指定code，返回该股票历年所有的财报信息。
	* 仅指定date，返回该日期所有股票的财报信息
	:notice :
    ————-每股指标—————————–
    | 1–基本每股收益 | 2–扣除非经常性损益每股收益 | 3–每股未分配利润 | 4–每股净资产 |
    | 5–每股资本公积金 | 6–净资产收益率 | 7–每股经营现金流量 |                 |
    ————-资产负债表—————————-
    | 8.货币资金 | 9.交易性金融资产 | 10.应收票据 | 11.应收账款 | 12.预付款项 | 13.其他应收款 |
    | 14.应收关联公司款 | 15.应收利息 | 16.应收股利 | 17.存货 | 18.其中：消耗性生物资产 |
    | 19.一年内到期的非流动资产 | 20.其他流动资产 | 21.流动资产合计 | 22.可供出售金融资产 |
    | 23.持有至到期投资 | 24.长期应收款 | 25.长期股权投资 | 26.投资性房地产 | 27.固定资产 |
    | 28.在建工程 | 29.工程物资 | 30.固定资产清理 | 31.生产性生物资产 | 32.油气资产 | 33.无形资产 |
    | 34.开发支出 | 35.商誉 | 36.长期待摊费用 | 37.递延所得税资产 | 38.其他非流动资产 | 39.非流动资产合计 |
    | 40.资产总计 | 41.短期借款 | 42.交易性金融负债 | 43.应付票据 | 44.应付账款 | 45.预收款项 | 46.应付职工薪酬 |
    | 47.应交税费 | 48.应付利息 | 49.应付股利 | 50.其他应付款 | 51.应付关联公司款 | 52.一年内到期的非流动负债
    | 53.其他流动负债 | 54.流动负债合计 | 55.长期借款 | 56.应付债券 | 57.长期应付款 | 58.专项应付款 |
    | 59.预计负债 | 60.递延所得税负债 | 61.其他非流动负债 | 62.非流动负债合计 | 63.负债合计 |
    | 64.实收资本（或股本）| 65.资本公积 | 66.盈余公积 | 67.减：库存股 | 68.未分配利润 |
    | 69.少数股东权益 | 70.外币报表折算价差 | 71.非正常经营项目收益调整 | 72.所有者权益（或股东权益）合计 |
    | 73.负债和所有者（或股东权益）合计 |

    ————-利润表—————————
    | 74.其中：营业收入 | 75.其中：营业成本 | 76.营业税金及附加 | 77.销售费用 | 78.管理费用 |
    | 79.堪探费用 | 80.财务费用 | 81.资产减值损失 | 82.加：公允价值变动净收益 | 83.投资收益 |
    | 84.其中：对联营企业和合营企业的投资收益 | 85.影响营业利润的其他科目 | 86.三、营业利润 |
    | 87.加：补贴收入 | 88.营业外收入 | 89.减：营业外支出 | 90.其中：非流动资产处置净损失 |
    | 91.加：影响利润总额的其他科目 | 92.四、利润总额 | 93.减：所得税 | 94.加：影响净利润的其他科目 |
    | 95.五、净利润 | 96.归属于母公司所有者的净利润 | 97.少数股东损益 |

    ——————现金流量表———————
    | 98.销售商品、提供劳务收到的现金 | 99.收到的税费返还 | 100.收到其他与经营活动有关的现金 |
    | 101.经营活动现金流入小计 | 102.购买商品、接受劳务支付的现金 | 103.支付给职工以及为职工支付的现金 |
    | 104.支付的各项税费 | 105.支付其他与经营活动有关的现金 | 106.经营活动现金流出小计 |
    | 107.经营活动产生的现金流量净额 | 108.收回投资收到的现金 | 109.取得投资收益收到的现金 |
    | 110.处置固定资产、无形资产和其他长期资产收回的现金净额 | 111.处置子公司及其他营业单位收到的现金净额 |
    | 112.收到其他与投资活动有关的现金 | 113.投资活动现金流入小计 |
    | 114.购建固定资产、无形资产和其他长期资产支付的现金 | 115.投资支付的现金 |
    | 116.取得子公司及其他营业单位支付的现金净额 | 117.支付其他与投资活动有关的现金 |
    | 118.投资活动现金流出小计 | 119.投资活动产生的现金流量净额 | 120.吸收投资收到的现金 |
    | 121.取得借款收到的现金 | 122.收到其他与筹资活动有关的现金 | 123.筹资活动现金流入小计 |
    | 124.偿还债务支付的现金 | 125.分配股利、利润或偿付利息支付的现金 | 126.支付其他与筹资活动有关的现金 |
    | 127.筹资活动现金流出小计 | 128.筹资活动产生的现金流量净额 | 129.四、汇率变动对现金的影响 |
    | 130.四(2)、其他原因对现金的影响 | 131.五、现金及现金等价物净增加额 | 132.期初现金及现金等价物余额 |
    | 133.期末现金及现金等价物余额 | 134.净利润 | 135.加：资产减值准备 |
    | 136.固定资产折旧、油气资产折耗、生产性生物资产折旧 | 137.无形资产摊销 | 138.长期待摊费用摊销 |
    | 139.处置固定资产、无形资产和其他长期资产的损失 | 140.固定资产报废损失 | 141.公允价值变动损失 |
    | 142.财务费用 | 143.投资损失 | 144.递延所得税资产减少 | 145.递延所得税负债增加 | 146.存货的减少 |
    | 147.经营性应收项目的减少 | 148.经营性应付项目的增加 | 149.其他 | 150.经营活动产生的现金流量净额2 |
    | 151.债务转为资本 | 152.一年内到期的可转换公司债券 | 153.融资租入固定资产 | 154.现金的期末余额 |
    | 155.减：现金的期初余额 | 156.加：现金等价物的期末余额 | 157.减：现金等价物的期初余额 |
    | 158.现金及现金等价物净增加额 |

    ———————偿债能力分析————————
    | 159.流动比率 | 160.速动比率 | 161.现金比率 | 162.利息保障倍数 | 163.非流动负债比率 |
    | 164.流动负债比率 | 165.现金到期债务比率 | 166.有形资产净值债务率 | 167.权益乘数(%) |
    | 168.股东的权益/负债合计(%) | 169.有形资产/负债合计(%) | 170.经营活动产生的现金流量净额/负债合计(%) |
    | 171.EBITDA/负债合计(%) |

    ———————经营效率分析————————
    | 172.应收帐款周转率 | 173.存货周转率 | 174.运营资金周转率 | 175.总资产周转率 | 176.固定资产周转率 |
    | 177.应收帐款周转天数 | 178.存货周转天数 | 179.流动资产周转率 | 180.流动资产周转天数 |
    | 181.总资产周转天数 | 182.股东权益周转率 |

    ———————发展能力分析————————
    | 183.营业收入增长率 | 184.净利润增长率 | 185.净资产增长率 | 186.固定资产增长率 | 187.总资产增长率 |
    | 188.投资收益增长率 | 189.营业利润增长率 | 190.暂无 | 191.暂无 | 192.暂无 |

    ———————获利能力分析————————
    | 193.成本费用利润率(%) | 194.营业利润率 | 195.营业税金率 | 196.营业成本率 | 197.净资产收益率 |
    | 198.投资收益率 | 199.销售净利率(%) | 200.总资产报酬率 | 201.净利润率 | 202.销售毛利率(%) |
    | 203.三费比重 | 204.管理费用率 | 205.财务费用率 | 206.扣除非经常性损益后的净利润 |
    | 207.息税前利润(EBIT) | 208.息税折旧摊销前利润(EBITDA) | 209.EBITDA/营业总收入(%) |

    ———————资本结构分析———————
    | 210.资产负债率(%) | 211.流动资产比率 | 212.货币资金比率 | 213.存货比率 | 214.固定资产比率 |
    | 215.负债结构比 | 216.归属于母公司股东权益/全部投入资本(%) | 217.股东的权益/带息债务(%) |
    | 218.有形资产/净债务(%) |

    ———————现金流量分析———————
    | 219.每股经营性现金流(元) | 220.营业收入现金含量(%) | 221.经营活动产生的现金流量净额/经营活动净收益(%) |
    | 222.销售商品提供劳务收到的现金/营业收入(%) | 223.经营活动产生的现金流量净额/营业收入 | 224.资本支出/折旧和摊销 |
    | 225.每股现金流量净额(元) | 226.经营净现金比率（短期债务） | 227.经营净现金比率（全部债务） |
    | 228.经营活动现金净流量与净利润比率 | 229.全部资产现金回收率 |

    ———————单季度财务指标———————
    | 230.营业收入 | 231.营业利润 | 232.归属于母公司所有者的净利润 | 233.扣除非经常性损益后的净利润 |
    | 234.经营活动产生的现金流量净额 | 235.投资活动产生的现金流量净额 | 236.筹资活动产生的现金流量净额 |
    | 237.现金及现金等价物净增加额 |

    ———————股本股东———————
    | 238.总股本 | 239.已上市流通A股 | 240.已上市流通B股 | 241.已上市流通H股 | 242.股东人数(户) |
    | 243.第一大股东的持股数量 | 244.十大流通股东持股数量合计(股) | 245.十大股东持股数量合计(股) |

    ———————机构持股———————
    | 246.机构总量（家） | 247.机构持股总量(股) | 248.QFII机构数 | 249.QFII持股量 | 250.券商机构数 |
    | 251.券商持股量 | 252.保险机构数 | 253.保险持股量 | 254.基金机构数 | 255.基金持股量 | 256.社保机构数 |
    | 257.社保持股量 | 258.私募机构数 | 259.私募持股量 | 260.财务公司机构数 | 261.财务公司持股量 |
    | 262.年金机构数 | 263.年金持股量 |

    ———————新增指标———————
    | 264.十大流通股东中持有A股合计(股) [注：季度报告中，若股东同时持有非流通A股性质的股份(如同时持有流通A股和流通B股）,
      指标264取的是包含同时持有非流通A股性质的流通股数] |
 */
func ReportList(conf comm.IConfigure, code, date string) dataframe.DataFrame{
	var allDateList []string
	dirList, err := ioutil.ReadDir(fmt.Sprintf("%s%s", conf.GetApp().DataPath, conf.GetTdx().Files.StockReport))
	if err != nil {
		return dataframe.DataFrame{Err:fmt.Errorf("遍历文件失败, Err=%v", err)}
	}

	for _, v := range dirList {
		fileName := v.Name()
		fileMode := v.Mode()
		if 0 == strings.Index(fileName, ".") || fileMode.IsDir() {
			continue
		}

		splitFileName := strings.Split(fileName, ".")
		runeFileName := []rune(splitFileName[0])
		runeDate := runeFileName[4:]

		allDateList = append(allDateList, string(runeDate[:]))
	}

	if len(date) > 0 {
		idx := utils.FindInStringSlice(date, allDateList)
		if 0 > idx {
			// 不存在指定的日期
			return dataframe.DataFrame{Err:fmt.Errorf("指定的日期不存在, Err=%v", err)}
		}
		nDateItem, _ := strconv.Atoi(date)
		return reportList(conf, code, nDateItem)
	}

	var resultDF dataframe.DataFrame

	for _, current := range allDateList {
		nDateItem, _ := strconv.Atoi(current)
		itemDf := reportList(conf, code, nDateItem)
		if itemDf.Err != nil { continue }

		if resultDF.Nrow() <= 0 {
			resultDF = itemDf.Copy()
		} else {
			resultDF = resultDF.RBind(itemDf)
		}

	}

	return resultDF
}
