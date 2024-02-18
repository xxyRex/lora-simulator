import sys
import secrets

def generate_random_string():
    random_bytes = secrets.token_bytes(16)
    random_string = ''.join([f'{b:02x}' for b in random_bytes])
    return random_string

def generate_devices(num: int):
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
        for i in range(1, 1 + num):
            newEUI = baseEUI + i
            newEUIStr = hex(newEUI)[2:]
            items[0] = newEUIStr
            items[1] = newEUIStr
            items[2] = newEUIStr
            items[7] = generate_random_string()
            dst.write(",".join(items) + "\n")

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage python3 generate_devices.py 2000(quantity of devices)")
    else:
        num = int(sys.argv[1])
        generate_devices(num)