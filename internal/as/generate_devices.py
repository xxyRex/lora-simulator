
def generate_devices():
    with open("base_devices_export.csv")  as src, open("devices_import.csv", mode="+w") as dst:
        readLines = src.readlines()
        baseLine = readLines[1]
        dst.write(readLines[0])
        items = baseLine.split(",")
        for i in items:
            print(i)
        baseEUI = int(items[0], 16)
        print(baseEUI)
        print(hex(baseEUI)[2:])
        for i in range(1, 100):
            newEUI = baseEUI + i
            newEUIStr = hex(newEUI)[2:]
            items[0] = newEUIStr
            items[1] = newEUIStr
            items[2] = newEUIStr
            dst.write(",".join(items) + "\n")

if __name__ == "__main__":
    generate_devices()