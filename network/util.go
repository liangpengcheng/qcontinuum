package network

import (
	"net/http"

	"github.com/liangpengcheng/qcontinuum/base"
)

// PaintAllValues 打印所有参数
func PaintAllValues(r *http.Request) {
	base.LogDebug(r.Form.Encode())
}
