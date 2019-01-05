package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"

	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"
)

var mac [6]byte
var dstSteps = flag.Uint("s", 66666, "你要刷的步数")

//cmdReadBDAddr实现了一个用于演示LnxSendHCIRawCommand()的cmd.CmdParam
type cmdReadBDAddr struct{}

func (c cmdReadBDAddr) Marshal(b []byte) {}
func (c cmdReadBDAddr) Opcode() int      { return 0x1009 }
func (c cmdReadBDAddr) Len() int         { return 0 }

//使用LnxSendHCIRawCommand()来获取bd地址（用于演示）
func getBdAddr(dev gatt.Device) {
	rsp := bytes.NewBuffer(nil)
	if err := dev.Option(gatt.LnxSendHCIRawCommand(&cmdReadBDAddr{}, rsp)); err != nil {
		fmt.Printf("无法发送HCI命令, 错误: %s", err)
	}
	b := rsp.Bytes()
	if b[0] != 0 {
		fmt.Printf("无法通过HCI命令获取bd地址, 状态: %d", b[0])
	}
	log.Printf("设备地址: %02X:%02X:%02X:%02X:%02X:%02X", b[6], b[5], b[4], b[3], b[2], b[1])

	mac[0] = b[6]
	mac[1] = b[5]
	mac[2] = b[4]
	mac[3] = b[3]
	mac[4] = b[2]
	mac[5] = b[1]
}

func main() {
	flag.Parse()
	if *dstSteps > 100000 {
		fmt.Println("步数不能大于10万步！")
		return
	}
	// （小端模式）字节序低位优先
	// 01(数据类型为步数)         10 27 00(0x015b38 = 88888)
	//stepsInfo := []byte{0x01, 0x38, 0x5b, 0x01}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(*dstSteps))
	stepsInfo := []byte{0x01}
	stepsInfo = append(stepsInfo, buf[:3]...)

	const (
		flagLimitedDiscoverable = 0x01 // 限制性可发现模式
		flagGeneralDiscoverable = 0x02 // 常规可发现模式
		flagLEOnly              = 0x04 // 不支持br/edr。由LMP Feature Mask Definitions功能的第37位掩码定义（第0页）
		flagBothController      = 0x08 // 同步LE和 BR/EDR到同一设备总线 (控制器).
		flagBothHost            = 0x10 // 同步LE和 BR/EDR到同一设备总线(主机).
	)

	const (
		wxServiceUUID = 0xFEE7 // 微信服务标志0xFEE7

		wxChWriteUUID    = 0xFEC7 // 微信服务写特征值
		wxChIndicateUUID = 0xFEC8 // 微信的标志字符
		wxChReadUUID     = 0xFEC9 // 微信读特征值

		wxChPedometerUUID = 0xFEA1 // 微信计步器特征值
		wxChTargetUUID    = 0xFEA2 // 微信目标特征值
	)

	//生成设备
	dev, err := gatt.NewDevice(option.DefaultServerOptions...)
	if err != nil {
		log.Fatalf("打开设备失败, 错误: %s", err)
	}

	// 向设备中注册可选处理程序
	dev.Handle(
		gatt.CentralConnected(func(c gatt.Central) { fmt.Println("连接: ", c.ID()) }),
		gatt.CentralDisconnected(func(c gatt.Central) { fmt.Println("断开: ", c.ID()) }),
	)

	// 用于监视设备状态的处理程序。
	onStateChanged := func(dev gatt.Device, state gatt.State) {
		fmt.Printf("状态: %s\n", state)
		switch state {
		case gatt.StatePoweredOn:
			getBdAddr(dev)

			//获取服务
			s0 := gatt.NewService(gatt.UUID16(wxServiceUUID))

			//添加计步器特征值
			c0 := s0.AddCharacteristic(gatt.UUID16(wxChPedometerUUID))
			c0.HandleReadFunc(
				func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
					log.Println("读取: 计步器特性值")
					rsp.Write(stepsInfo)
				})
			c0.HandleNotifyFunc(
				func(r gatt.Request, n gatt.Notifier) {
					go func() {
						n.Write(stepsInfo)
						log.Printf("告知: 计步器特性值")
					}()
				})

			// 添加目标特征值
			c1 := s0.AddCharacteristic(gatt.UUID16(wxChTargetUUID))
			c1.HandleReadFunc(
				func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
					log.Println("读取: 目标特征值")
					rsp.Write(stepsInfo)
				})
			c1.HandleNotifyFunc(
				func(r gatt.Request, n gatt.Notifier) {
					go func() {
						n.Write(stepsInfo)
						log.Printf("告知: 目标特征值")
					}()
				})
			c1.HandleWriteFunc(
				func(r gatt.Request, data []byte) (status byte) {
					log.Println("写入目标特征值:", string(data))
					return gatt.StatusSuccess
				})

			// 添加读特征值
			c2 := s0.AddCharacteristic(gatt.UUID16(wxChReadUUID))
			c2.HandleReadFunc(
				func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
					log.Println("读取: 读特征值")
					rsp.Write(mac[:])
				})

			// 添加服务
			dev.AddService(s0)

			// 广播设备名和服务的UUID
			a := &gatt.AdvPacket{}
			a.AppendFlags(flagGeneralDiscoverable | flagLEOnly)
			a.AppendUUIDFit([]gatt.UUID{s0.UUID()})
			a.AppendName("WeixinBLE")

			// company id 和 data, MAC 地址
			// https://www.bluetooth.com/specifications/assigned-numbers/company-identifiers
			a.AppendManufacturerData(0x2333, mac[:])
			dev.Advertise(a)

		default:
		}
	}

	dev.Init(onStateChanged)
	select {}
}

//http://www.wowotech.net/bluetooth/weixin_ble_1.html
