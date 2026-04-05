#!/bin/bash
# 模拟 protoc 生成 .pb.cc/.pb.h 和 .dd 文件
BASENAME=$(basename "$1" .proto)
touch "${BASENAME}.pb.cc"
touch "${BASENAME}.pb.h"
cat > "${BASENAME}.pb.dd" <<EOF
build ${BASENAME}.pb.o: cxx ${BASENAME}.pb.cc
EOF