#!/bin/bash

LORA_AS_PATH=$1
LORA_SIMULATOR_PATH="../"
PACKAGE_NAME="as_api"
API_API_PATH="$LORA_SIMULATOR_PATH/"internal/$PACKAGE_NAME""

if [ -z "$LORA_AS_PATH" ]; then
    echo "Usage: $0 <path_to_loraas>"
    exit 1
fi

cur_dir=$(pwd)

TARGET_DIR="$LORA_AS_PATH/src/github.com/brocaar/lora-app-server"

# 检查目标目录是否存在
if [ ! -d "$TARGET_DIR" ]; then
    echo "错误: 找不到目录 $TARGET_DIR"
    exit 1
fi

# 切换目录并执行gen.sh
cd "$TARGET_DIR" || { echo "切换目录失败"; exit 1; }
echo "已切换到目录: $(pwd)"

PATH=$PATH:$LORA_AS_PATH/bin
export GOPATH=$LORA_AS_PATH

go generate api/api.go

if [ $? -ne 0 ]; then
    echo "执行gen.sh失败"
    cd "$cur_dir"
    exit 1
fi

# 返回原始目录
cd "$cur_dir" || { echo "返回原始目录失败"; exit 1; }

# 清空目标目录，确保删除已经不存在的API对应的文件
if [ -d $API_API_PATH ]; then
    echo "清空旧的API客户端目录..."
    rm -rf $API_API_PATH
fi

mkdir -p $API_API_PATH

swagger mixin $LORA_AS_PATH/src/github.com/brocaar/lora-app-server/api/swagger/*.swagger.json -o api.swagger.json

python3 fix_mixin_operation_id.py
python3 convert_swagger_to_camel_case.py api.fixed.json api.fixed.camel.json

swagger generate client \
    -f "api.fixed.camel.json" \
    -A "$PACKAGE_NAME" \
    -t "$API_API_PATH" \
    --skip-validation
if [ $? -eq 0 ]; then
    echo "✅ 客户端代码生成成功！输出目录: internal/$PACKAGE_NAME"
    echo "现在您可以导入此客户端包并使用它来调用API"
else
    echo "❌ 客户端代码生成失败"
    exit 1
fi 

go mod tidy
