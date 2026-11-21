//go:build linux

package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const notifierInterface = "org.kde.StatusNotifierItem"
const menuInterface = "com.canonical.dbusmenu"
const watcherName = "org.kde.StatusNotifierWatcher"

type trayItem struct { service *linuxService }
type menuItem struct { service *linuxService }
type iconPixmap struct { Width, Height int32; Pixels []byte }
type menuLayout struct { ID int32; Properties map[string]dbus.Variant; Children []dbus.Variant }
type menuGroup struct { ID int32; Properties map[string]dbus.Variant }

func (s *linuxService) startTray() error {
	name := linuxAppName()
	reply, err := s.bus.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner { return fmt.Errorf("Не удалось зарегистрировать tray-сервис") }
	item, menu := &trayItem{s}, &menuItem{s}
	if err := s.bus.Export(item, "/StatusNotifierItem", notifierInterface); err != nil { return err }
	if err := s.bus.Export(menu, "/Menu", menuInterface); err != nil { return err }
	pixels := make([]byte,32*32*4)
	for y:=0;y<32;y++ { for x:=0;x<32;x++ {
		i:=(y*32+x)*4
		if x>=4 && x<28 && y>=5 && y<27 { pixels[i],pixels[i+1],pixels[i+2],pixels[i+3]=255,45,106,78 }
		if x>=9 && x<23 && (y==12 || y==18) { pixels[i],pixels[i+1],pixels[i+2],pixels[i+3]=255,242,249,239 }
	} }
	_, err = prop.Export(s.bus, "/StatusNotifierItem", prop.Map{notifierInterface: {
		"Category": {Value:"ApplicationStatus",Emit:prop.EmitConst},
		"Id": {Value:"skrinpolka",Emit:prop.EmitConst},
		"Title": {Value:"ScreenShooter Pro",Emit:prop.EmitConst},
		"Status": {Value:"Active",Emit:prop.EmitTrue},
		"WindowId": {Value:uint32(0),Emit:prop.EmitConst},
		"IconName": {Value:"",Emit:prop.EmitConst},
		"IconPixmap": {Value:[]iconPixmap{{32,32,pixels}},Emit:prop.EmitConst},
		"ItemIsMenu": {Value:false,Emit:prop.EmitConst},
		"Menu": {Value:dbus.ObjectPath("/Menu"),Emit:prop.EmitConst},
	}})
	if err != nil { return err }
	_, err = prop.Export(s.bus, "/Menu", prop.Map{menuInterface: {
		"Version": {Value:uint32(4),Emit:prop.EmitConst},
		"TextDirection": {Value:"ltr",Emit:prop.EmitConst},
		"Status": {Value:"normal",Emit:prop.EmitConst},
		"IconThemePath": {Value:[]string{},Emit:prop.EmitConst},
	}})
	if err != nil { return err }
	s.bus.Export(introspect.NewIntrospectable(&introspect.Node{Interfaces:[]introspect.Interface{
		{Name:notifierInterface,Methods:introspect.Methods(item)},prop.IntrospectData,
	}}), "/StatusNotifierItem", "org.freedesktop.DBus.Introspectable")
	s.bus.Export(introspect.NewIntrospectable(&introspect.Node{Interfaces:[]introspect.Interface{
		{Name:menuInterface,Methods:introspect.Methods(menu)},prop.IntrospectData,
	}}), "/Menu", "org.freedesktop.DBus.Introspectable")
	register := func() error {
		ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
		return s.bus.Object(watcherName,"/StatusNotifierWatcher").CallWithContext(ctx,watcherName+".RegisterStatusNotifierItem",0,name).Err
	}
	// Keep watching even if no watcher exists at launch: enabling a shell
	// extension later can make the icon available without restarting the app.
	channel:=make(chan *dbus.Signal,8)
	s.bus.Signal(channel)
	if err:=s.bus.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"),dbus.WithMatchMember("NameOwnerChanged"),dbus.WithMatchArg(0,watcherName));err!=nil { s.bus.RemoveSignal(channel);return err }
	go func(){ for signal:=range channel {
		if signal==nil || len(signal.Body)!=3 {continue}
		owner,_:=signal.Body[2].(string)
		feature:=Feature{Reason:"Трей недоступен: служба оболочки остановлена"}
		if owner!="" { if err:=register();err==nil {feature=Feature{Available:true,Reason:"StatusNotifierItem"}} else {feature.Reason=err.Error()} }
		s.mu.Lock();s.caps.Tray=feature;s.mu.Unlock();s.changed()
	} }()
	if err:=register();err!=nil {return fmt.Errorf("Оболочка не предоставляет StatusNotifierItem: %w",err)}
	s.caps.Tray=Feature{Available:true,Reason:"StatusNotifierItem"}
	return nil
}

func (t *trayItem) Activate(int32,int32) *dbus.Error { if t.service.actions.OpenLibrary!=nil {go t.service.actions.OpenLibrary()};return nil }
func (t *trayItem) SecondaryActivate(int32,int32) *dbus.Error { if t.service.actions.Capture!=nil {go t.service.actions.Capture()};return nil }
func (t *trayItem) ContextMenu(int32,int32) *dbus.Error { return nil }
func (t *trayItem) Scroll(int32,string) *dbus.Error { return nil }

func menuProperties(id int32) map[string]dbus.Variant {
	labels:=map[int32]string{1:"Выделить область",5:"Весь экран",2:"Открыть библиотеку",3:"Настройки",4:"Выйти"}
	if id==0 {return map[string]dbus.Variant{"children-display":dbus.MakeVariant("submenu")}}
	return map[string]dbus.Variant{"label":dbus.MakeVariant(labels[id]),"enabled":dbus.MakeVariant(true),"visible":dbus.MakeVariant(true)}
}

func (m *menuItem) GetLayout(parent int32, depth int32, names []string) (uint32,menuLayout,*dbus.Error) {
	layout:=menuLayout{ID:parent,Properties:menuProperties(parent),Children:[]dbus.Variant{}}
	if parent==0 && depth!=0 {for _,id:=range []int32{1,5,2,3,4} {layout.Children=append(layout.Children,dbus.MakeVariant(menuLayout{ID:id,Properties:menuProperties(id),Children:[]dbus.Variant{}}))}}
	return 1,layout,nil
}

func (m *menuItem) GetGroupProperties(ids []int32,names []string) ([]menuGroup,*dbus.Error) {
	if len(ids)==0 {ids=[]int32{0,1,5,2,3,4}}
	result:=[]menuGroup{};for _,id:=range ids {result=append(result,menuGroup{id,menuProperties(id)})};return result,nil
}

func (m *menuItem) GetProperty(id int32,name string) (dbus.Variant,*dbus.Error) {
	value,ok:=menuProperties(id)[name];if !ok {return dbus.Variant{},dbus.MakeFailedError(fmt.Errorf("Неизвестное свойство"))};return value,nil
}

func (m *menuItem) Event(id int32,event string,data dbus.Variant,timestamp uint32) *dbus.Error {
	if event!="clicked" {return nil}
	var action func()
	switch id {case 1:action=m.service.actions.Capture;case 5:action=m.service.actions.CaptureScreen;case 2:action=m.service.actions.OpenLibrary;case 3:action=m.service.actions.OpenSettings;case 4:action=m.service.actions.Quit}
	if action!=nil {go action()};return nil
}

func (m *menuItem) AboutToShow(int32) (bool,*dbus.Error) {return false,nil}
