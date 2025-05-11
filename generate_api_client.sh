#!/bin/bash

LORA_AS_PATH=$1

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

PACKAGE_NAME="as_api"

mkdir -p internal/$PACKAGE_NAME

swagger mixin $LORA_AS_PATH/src/github.com/brocaar/lora-app-server/api/swagger/*.swagger.json -o api.swagger.json

python3 fix_mixin_operation_id.py

swagger generate client \
    -f "api.fixed.json" \
    -A "$PACKAGE_NAME" \
    -t "$cur_dir/internal/$PACKAGE_NAME" \
    --skip-validation
if [ $? -eq 0 ]; then
    echo "✅ 客户端代码生成成功！输出目录: internal/$PACKAGE_NAME"
    echo "现在您可以导入此客户端包并使用它来调用API"
else
    echo "❌ 客户端代码生成失败"
    exit 1
fi 

go mod tidy
