function Encode(fPort, object) {
  var bytes = [];
  
  // IPSO VERSION
  if (object.ipso_version) {
      bytes.push(0xff);
      bytes.push(0x01);
      bytes = bytes.concat(writeProtocolVersion(object.ipso_version));
  }
  
  // HARDWARE VERSION
  if (object.hardware_version) {
      bytes.push(0xff);
      bytes.push(0x09);
      bytes = bytes.concat(writeHardwareVersion(object.hardware_version));
  }
  
  // FIRMWARE VERSION
  if (object.firmware_version) {
      bytes.push(0xff);
      bytes.push(0x0a);
      bytes = bytes.concat(writeFirmwareVersion(object.firmware_version));
  }
  
  // BATTERY
  if (object.hasOwnProperty('battery')) {
      bytes.push(0x01);
      bytes.push(0x75);
      bytes.push(object.battery);
  }
  
  // TEMPERATURE
  if (object.hasOwnProperty('temperature')) {
      bytes.push(0x03);
      bytes.push(0x67);
      bytes = bytes.concat(writeInt16LE(object.temperature * 10));
  }
  
  // HUMIDITY
  if (object.hasOwnProperty('humidity')) {
      bytes.push(0x04);
      bytes.push(0x68);
      bytes.push(object.humidity * 2);
  }

  if (object.hasOwnProperty('lorawan_class')) {
    bytes.push(0xff);
    bytes.push(0x0f);
    bytes.push(object.lorawan_class);
}

  return bytes;
}

function writeInt16LE(value) {
  var val = Math.floor(value);
  return [(val & 0xff), (val >> 8) & 0xff];
}

function writeProtocolVersion(version) {
  var parts = version.substr(1).split('.');
  var major = parseInt(parts[0]);
  var minor = parseInt(parts[1]);
  return [(major << 4) | minor];
}

function writeHardwareVersion(version) {
  var parts = version.substr(1).split('.');
  var major = parseInt(parts[0]);
  var minor = parseInt(parts[1]);
  return [major, minor << 4];
}

function writeFirmwareVersion(version) {
  var parts = version.substr(1).split('.');
  var major = parseInt(parts[0]);
  var minor = parseInt(parts[1]);
  return [major, minor];
}

// Usage:
var encodedBytes = Encode(1, {
  ipso_version:"1.1",
  battery: 100,
  temperature: 50.6,
  humidity: 11.5,
  lorawan_class: 0
});