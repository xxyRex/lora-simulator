package as

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
)

type LoRaWANDeviceCfg struct {
	Id            int
	Name          string
	Description   string
	DevEUI        string
	Application   string
	DeviceProfile string
	AppKey        string
	DevAddr       string
	AppSKey       string
	NwkSKey       string
	FPort         string
	PayloadCodec  string
}

type CsvTable struct {
	FileName string
	Records  []CsvRecord
}

type CsvRecord struct {
	Record map[string]string
}

func (c *CsvRecord) GetInt(field string) int {
	var r int
	var err error
	if r, err = strconv.Atoi(c.Record[field]); err != nil {
		fmt.Println(err)
		panic(err)
	}
	return r
}

func (c *CsvRecord) GetString(field string) string {
	data, ok := c.Record[field]
	if ok {
		return data
	} else {
		fmt.Println("Get fileld failed! fileld:", field)
		return ""
	}
}

func loadCsvCfg(filename string, row int) *CsvTable {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	defer file.Close()

	reader := csv.NewReader(file)
	if reader == nil {
		fmt.Println("NewReader return nil, file:", file)
		return nil
	}
	records, err := reader.ReadAll()
	if err != nil {
		fmt.Println(err)
		return nil
	}
	if len(records) < row {
		fmt.Println(filename, " is empty")
		return nil
	}
	colNum := len(records[0])
	recordNum := len(records)
	var allRecords []CsvRecord
	for i := row; i < recordNum; i++ {
		record := &CsvRecord{make(map[string]string)}
		for k := 0; k < colNum; k++ {
			record.Record[records[0][k]] = records[i][k]
		}
		allRecords = append(allRecords, *record)
	}
	var result = &CsvTable{
		filename,
		allRecords,
	}
	return result
}

func LoadLoRaWANDevCfg(filepath string, row int) (bool, map[int]*LoRaWANDeviceCfg) {
	var g_allLoRaWANDevCfg map[int]*LoRaWANDeviceCfg
	var result = loadCsvCfg(filepath, row)
	if result == nil {
		os.Remove(filepath)
		fmt.Printf("file format error!\n")
		return false, g_allLoRaWANDevCfg
	}
	g_allLoRaWANDevCfg = make(map[int]*LoRaWANDeviceCfg)
	for id, record := range result.Records {
		loraWANDev := &LoRaWANDeviceCfg{
			id + 1,
			record.GetString("name"),
			record.GetString("description"),
			record.GetString("deveui"),
			record.GetString("application"),
			record.GetString("deviceprofile"),
			record.GetString("appkey"),
			record.GetString("devaddr"),
			record.GetString("appskey"),
			record.GetString("nwkskey"),
			record.GetString("fport"),
			record.GetString("payloadcodec"),
		}
		g_allLoRaWANDevCfg[id] = loraWANDev
	}
	return true, g_allLoRaWANDevCfg
}
