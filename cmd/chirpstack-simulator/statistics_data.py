import re
import sys
import fileinput

def statistics_lns_uplink():
    with open("devices_import.csv")  as file, open("lora-app-server.log") as file2, open("statistics_uplink_result.log", "w") as result:
        for line in file:
            items = line.split(",")
            eui = items[0]
            count = 0
            file2.seek(0)
            for line2 in file2:
                if re.search(f'application/1/device/{eui}/rx', line2):
                    count = count + 1
            str = f"接收到 DevEUI: {eui} 上行数据 {count} 条\n"
            result.write(str)

def statistics_simulator_uplink():
    with open("devices_import.csv")  as file, open("statistics.csv", "w") as resultFile:
        resultFile.write("eui,devAddr,node uplink count,node downlink count,lns uplink count\n")
        for devItem in file:
            eui = devItem.split(",")[0]
            if eui == "deveui":
                continue
            patternUp = re.compile(
                r'dataUpCount:=(.+) dev_addr=(.+) dev_eui=' + eui
            )
            patternDown = re.compile(
                r'datadownCount=(.+) dev_eui=' + eui
            )
            devAddr = ""
            dataUplinkCount = ""
            dataDownlinkCount = ""
            lnsUplinkCount = 0
            with fileinput.input(files="simulator.log") as logFile:
                upFound = False
                downFound = False
                for line in reversed(list(logFile)):
                    if not upFound:
                        matchUp = patternUp.search(line)
                    if not downFound:
                        matchDown = patternDown.search(line)
                    if not upFound and matchUp:
                        dataUplinkCount = matchUp.group(1)
                        devAddr = matchUp.group(2)
                        upFound = True
                    if not downFound and matchDown:
                        dataDownlinkCount = matchDown.group(1)
                        downFound = True
                    if upFound and downFound:
                        break
            with open("lora-app-server.log") as appLogFile:
                for line in appLogFile:
                    if re.search(f'application/1/device/{eui}/rx', line):
                        lnsUplinkCount = lnsUplinkCount + 1
            resultFile.write(eui + "," + devAddr + "," + dataUplinkCount + "," + dataDownlinkCount + ',' + str(lnsUplinkCount) + "\n")

class Result:
    def __init__(self, devAddr, count) -> None:
        self.devAddr = devAddr
        self.count = count

def filter_simulator_log(num: int):
    with fileinput.input(files="simulator.log") as logFile, open("statistics.csv", mode="w") as resultFile:
        resultFile.write("eui,devAddr,node uplink count,node downlink count,lns uplink count\n")
        patternUp = re.compile(
            r'dataUpCount:=(.+) dev_addr=(.+) dev_eui=(.+)'
        )
        patternDown = re.compile(
            r'datadownCount=(.+) dev_eui=(.+) f_cnt='
        )

        mapUp = {}

        mapDown = {}
        
        for line in reversed(list(logFile)):
            upFound = False
            downFound = False

            matchUp = patternUp.search(line)
            matchDown = patternDown.search(line)

            if matchUp:
                upFound = True
                if matchUp.group(3) not in mapUp:
                    result = Result(matchUp.group(2), matchUp.group(1))
                    mapUp[matchUp.group(3)] = result
                    print("len(mapUp): ", len(mapUp))
            if matchDown:
                if matchDown.group(2) not in mapDown:
                    result = Result("", matchDown.group(1))
                    mapDown[matchDown.group(2)] = result
                    print("len(mapDown): ", len(mapDown))

            if len(mapUp) == num and len(mapDown) == num:
                break

        for key, value in mapUp.items():
            up_count = value.count
            down_count = mapDown[key].count if key in mapDown else '0'  # Use '0' or an appropriate default value
            resultFile.write(f"{key},{value.devAddr},{up_count},{down_count},\n")

        

if __name__ == "__main__":
    # ret = statistics_simulator_uplink()
    if len(sys.argv) != 2:
        print("Usage python3 statistics_data.py 2000(quantity of devices)")
    else:
        num = int(sys.argv[1])
        filter_simulator_log(num)