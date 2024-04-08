/**
 * Payload Encoder for Milesight Network Server
 *
 * Copyright 2023 Milesight IoT
 *
 * @product WT201
 */
function Encode(fPort, decoded) {
    var bytes = [];

    // IPSO VERSION
    if (decoded.ipso_version) {
        bytes.push(0xff, 0x01);
        bytes.push(writeProtocolVersion(decoded.ipso_version));
    }

    // HARDWARE VERSION
    if (decoded.hardware_version) {
        bytes.push(0xff, 0x09);
        bytes = bytes.concat(writeHardwareVersion(decoded.hardware_version));
    }

    // FIRMWARE VERSION
    if (decoded.firmware_version) {
        bytes.push(0xff, 0x0a);
        bytes = bytes.concat(writeFirmwareVersion(decoded.firmware_version));
    }

    // DEVICE STATUS
    if (decoded.device_status !== undefined) {
        bytes.push(0xff, 0x0b, 1);
    }

    // LORAWAN CLASS TYPE
    if (decoded.lorawan_class !== undefined) {
        bytes.push(0xff, 0x0f, decoded.lorawan_class);
    }

    // SERIAL NUMBER
    if (decoded.sn) {
        bytes.push(0xff, 0x16);
        bytes = bytes.concat(writeSerialNumber(decoded.sn));
    }

    // TEMPERATURE
    if (decoded.temperature !== undefined) {
        bytes.push(0x03, 0x67);
        bytes = bytes.concat(writeInt16LE(decoded.temperature * 10));
    }

    // TEMPERATURE TARGET
    if (decoded.temperature_target !== undefined) {
        bytes.push(0x04, 0x67);
        bytes = bytes.concat(writeInt16LE(decoded.temperature_target * 10));
    }

    // TEMPERATURE CONTROL
    if (decoded.temperature_ctl_mode !== undefined && decoded.temperature_ctl_status !== undefined) {
        bytes.push(0x05, 0xe7);
        bytes.push(decoded.temperature_ctl_mode | (decoded.temperature_ctl_status << 4));
    }

    // FAN CONTROL
    if (decoded.fan_mode !== undefined && decoded.fan_status !== undefined) {
        bytes.push(0x06, 0xe8);
        bytes.push(decoded.fan_mode | (decoded.fan_status << 2));
    }

    // PLAN EVENT
    if (decoded.plan_event !== undefined) {
        bytes.push(0x07, 0xbc, decoded.plan_event);
    }

    // SYSTEM STATUS
    if (decoded.system_status !== undefined) {
        bytes.push(0x08, 0x8e, decoded.system_status);
    }

    // PLAN
    if (decoded.plan_schedule) {
        for (var i = 0; i < decoded.plan_schedule.length; i++) {
            var schedule = decoded.plan_schedule[i];
            bytes.push(0xff, 0xc9, schedule.type, schedule.index - 1, schedule.plan_enable);
            bytes.push(writeWeekRecycleSettings(schedule.week_recycle));
            var time_mins = parseInt(schedule.time.split(":")[0]) * 60 + parseInt(schedule.time.split(":")[1]);
            bytes = bytes.concat(writeUInt16LE(time_mins));
        }
    }

    // PLAN SETTINGS
    if (decoded.plan_settings) {
        for (var i = 0; i < decoded.plan_settings.length; i++) {
            var plan_setting = decoded.plan_settings[i];
            bytes.push(0xff, 0xc8, writePlanType(plan_setting.type), plan_setting.temperature_ctl_mode, plan_setting.fan_mode, plan_setting.temperature_target, plan_setting.temperature_error * 10);
        }
    }

    // WIRES
    if (decoded.wires !== undefined && decoded.ob_mode !== undefined) {
        bytes.push(0xff, 0xca);
        bytes = bytes.concat(writeWires(decoded.wires, decoded.ob_mode));
    }

    // TEMPERATURE MODE SUPPORT
    if (decoded.temperature_ctl_mode_enable !== undefined && decoded.temperature_ctl_status_enable !== undefined) {
        bytes.push(0xff, 0xcb);
        bytes.push(writeTemperatureCtlModeEnable(decoded.temperature_ctl_mode_enable));
        bytes = bytes.concat(writeTemperatureCtlStatusEnable(decoded.temperature_ctl_status_enable));
    }

    // TEMPERATURE ALARM
    if (decoded.temperature !== undefined && decoded.temperature_alarm !== undefined) {
        bytes.push(0x83, 0x67);
        bytes = bytes.concat(writeInt16LE(decoded.temperature * 10), decoded.temperature_alarm);
    }

    // HISTRORICAL DATA
    if (decoded.history) {
        for (var i = 0; i < decoded.history.length; i++) {
            var data = decoded.history[i];
            bytes.push(0x20, 0xce);
            bytes = bytes.concat(writeUInt32LE(data.timestamp));
            var value1 = (data.fan_mode | (data.fan_status << 2) | (data.system_status << 4) | ((data.temperature + 100) * 10 << 5)) & 0xffff;
            var value2 = (data.temperature_ctl_mode | (data.temperature_ctl_status << 2) | ((data.temperature_target + 100) * 10 << 5)) & 0xffff;
            bytes = bytes.concat(writeUInt16LE(value1), writeUInt16LE(value2));
        }
    }

    return bytes;
}

function writeUInt8(value) {
    return [value & 0xff];
}

function writeInt8LE(value) {
    return writeUInt8(value > 0x7f ? value - 0x100 : value);
}

function writeUInt16LE(value) {
    var bytes = [];
    bytes.push(value & 0xff);
    bytes.push((value >>> 8) & 0xff);
    return bytes;
}

function writeInt16LE(value) {
    var ref = value > 0x7fff ? value - 0x10000 : value;
    return writeUInt16LE(ref);
}

function writeUInt32LE(value) {
    var bytes = [];
    bytes.push(value & 0xff);
    bytes.push((value >>> 8) & 0xff);
    bytes.push((value >>> 16) & 0xff);
    bytes.push((value >>> 24) & 0xff);
    return bytes;
}

function writeInt32LE(value) {
    var ref = value > 0x7fffffff ? value - 0x100000000 : value;
    return writeUInt32LE(ref);
}

function writeProtocolVersion(version) {
    var major = parseInt(version.split(".")[0].slice(1));
    var minor = parseInt(version.split(".")[1]);
    return (major << 4) | minor;
}

function writeHardwareVersion(version) {
    var major = parseInt(version.split(".")[0].slice(1));
    var minor = parseInt(version.split(".")[1]);
    return [major, (minor << 4)];
}

function writeFirmwareVersion(version) {
    var major = parseInt(version.split(".")[0].slice(1));
    var minor = parseInt(version.split(".")[1]);
    return [major, minor];
}

function writeSerialNumber(sn) {
    var bytes = [];
    for (var i = 0; i < sn.length; i += 2) {
        bytes.push(parseInt(sn.substr(i, 2), 16));
    }
    return bytes;
}

function writePlanType(type) {
    switch (type) {
        case "wake":
            return 0x00;
        case "away":
            return 0x01;
        case "home":
            return 0x02;
        case "sleep":
            return 0x03;
        default:
            return 0x00;
    }
}

function writeFanMode(mode) {
    switch (mode) {
        case "auto":
            return 0x00;
        case "on":
            return 0x01;
        case "circulate":
            return 0x02;
        case "disable":
            return 0x03;
        default:
            return 0x00;
    }
}

function writeFanStatus(status) {
    switch (status) {
        case "standby":
            return 0x00;
        case "high speed":
            return 0x01;
        case "low speed":
            return 0x02;
        case "on":
            return 0x03;
        default:
            return 0x00;
    }
}

function writeSystemStatus(status) {
    switch (status) {
        case "off":
            return 0x00;
        case "on":
            return 0x01;
        default:
            return 0x00;
    }
}

function writeTemperatureCtlMode(mode) {
    switch (mode) {
        case "heat":
            return 0x00;
        case "em heat":
            return 0x01;
        case "cool":
            return 0x02;
        case "auto":
            return 0x03;
        default:
            return 0x00;
    }
}

function writeTemperatureCtlStatus(status) {
    switch (status) {
        case "standby":
            return 0x00;
        case "stage-1 heat":
            return 0x01;
        case "stage-2 heat":
            return 0x02;
        case "stage-3 heat":
            return 0x03;
        case "stage-4 heat":
            return 0x04;
        case "em heat":
            return 0x05;
        case "stage-1 cool":
            return 0x06;
        case "stage-2 cool":
            return 0x07;
        default:
            return 0x00;
    }
}

function writeWires(wires, ob_mode) {
    var wire1 = 0, wire2 = 0, wire3 = 0;
    for (var i = 0; i < wires.length; i++) {
        switch (wires[i]) {
            case "y1":
                wire1 |= 0x01;
                break;
            case "gh":
                wire1 |= 0x04;
                break;
            case "ob":
                wire1 |= 0x10;
                break;
            case "w1":
                wire1 |= 0x40;
                break;
            case "e":
                wire2 |= 0x01;
                break;
            case "di":
                wire2 |= 0x04;
                break;
            case "pek":
                wire2 |= 0x10;
                break;
            case "w2":
                wire2 |= 0x40;
                break;
            case "aux":
                wire2 |= 0x80;
                break;
            case "y2":
                wire3 |= 0x01;
                break;
            case "gl":
                wire3 |= 0x02;
                break;
        }
    }
    wire3 |= (ob_mode << 2);
    return [wire1, wire2, wire3];
}

function writeObMode(mode) {
    switch (mode) {
        case "cool":
            return 0x00;
        case "heat":
            return 0x01;
        default:
            return 0x00;
    }
}

function writeTemperatureCtlModeEnable(enable) {
    var value = 0;
    for (var i = 0; i < enable.length; i++) {
        switch (enable[i]) {
            case "heat":
                value |= 0x01;
                break;
            case "em heat":
                value |= 0x02;
                break;
            case "cool":
                value |= 0x04;
                break;
            case "auto":
                value |= 0x08;
                break;
        }
    }
    return value;
}

function writeTemperatureCtlStatusEnable(enable) {
    var heat_mode = 0, cool_mode = 0;
    for (var i = 0; i < enable.length; i++) {
        switch (enable[i]) {
            case "stage-1 heat":
                heat_mode |= 0x01;
                break;
            case "stage-2 heat":
                heat_mode |= 0x02;
                break;
            case "stage-3 heat":
                heat_mode |= 0x04;
                break;
            case "stage-4 heat":
                heat_mode |= 0x08;
                break;
            case "aux heat":
                heat_mode |= 0x10;
                break;
            case "stage-1 cool":
                cool_mode |= 0x01;
                break;
            case "stage-2 cool":
                cool_mode |= 0x02;
                break;
        }
    }
    return [heat_mode, cool_mode];
}

function writeWeekRecycleSettings(week_enable) {
    var value = 0;
    for (var i = 0; i < week_enable.length; i++) {
        switch (week_enable[i]) {
            case "Mon.":
                value |= 0x02;
                break;
            case "Tues.":
                value |= 0x04;
                break;
            case "Wed.":
                value |= 0x08;
                break;
            case "Thur.":
                value |= 0x10;
                break;
            case "Fri.":
                value |= 0x20;
                break;
            case "Sat.":
                value |= 0x40;
                break;
            case "Sun.":
                value |= 0x80;
                break;
        }
    }
    return value;
}

var decoded = {
    sn:"623465780367110",
    temperature: 29.3,
    temperature_target: 22.0,
    temperature_ctl_mode: 5,
    temperature_ctl_status: 6, // stage-1 cool
    system_status: 1,
    plan_schedule: [
        {
            type: 0, // wake
            index: 1,
            plan_enable: 1,
            week_recycle: ["Mon.", "Tues.", "Wed.", "Thur.", "Fri."],
            time: "7:00"
        }
    ],
    plan_settings: [
        {
            type: "wake",
            temperature_ctl_mode: 2, // heat
            fan_mode: 1, // on
            temperature_target: 22,
            temperature_error: 8.5
        }
    ]
};

var encodedBytes = Encode(null, decoded);

// console.log(encodedBytes)

// const decoder = require("./wt201-decoder");

// ret = decoder.Decode(1, encodedBytes);

// console.log(ret)