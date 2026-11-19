//go:build windows

package platform

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"skrinpolka/internal/core"
)

func (s *windowsService) CaptureRegion(ctx context.Context, region core.ScreenRegion) (Capture,error) {
	if err:=region.Validate();err!=nil{return Capture{},err}
	if region.DesktopTopology==""{return Capture{},fmt.Errorf("У тихой области нет сведений о дисплеях. Заново задай область в настройках")}
	var output *image.NRGBA
	err:=s.onThread(ctx,func()error{
		if s.overlay!=0{return fmt.Errorf("Уже выполняется выделение области")}
		frame,err:=readWindowsFrame();if err!=nil{return err};defer frame.close()
		if !region.MatchesDesktop(frame.x,frame.y,frame.width,frame.height)||region.DesktopTopology!=frame.topology{return fmt.Errorf("Дисплеи или масштаб изменились. Заново задай область тихого снимка в настройках")}
		x,y:=region.X-frame.x,region.Y-frame.y
		output=windowsRegion(frame,image.Rect(x,y,x+region.Width,y+region.Height))
		return nil
	})
	if err!=nil{return Capture{},err}
	if err:=ctx.Err();err!=nil{return Capture{},err}
	var encoded bytes.Buffer
	if err:=png.Encode(&encoded,output);err!=nil{return Capture{},err}
	return Capture{PNG:encoded.Bytes(),MonitorID:"windows-virtual-desktop",Scale:1,Region:&region},nil
}
