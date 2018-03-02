package main

import(
	"os"
	"log"
	"syscall"
	"time"
	"errors"
	"flag"
	"sync"
	"io/ioutil"
)

type LogCfg struct{
	sync.RWMutex
	logmap map[string][]string//{disk:path}
}


var(
	NoFileSysErr = errors.New("can not get file sys info")
	TypeCvtErr = errors.New("type convert error")

	cfgfile string
	cfgmtime time.Time
)


func getAccTime(info os.FileInfo)(time.Time,error){
	var ret time.Time
	sysinfo := info.Sys()
	if sysinfo == nil{
		return ret,NoFileSysErr
	}
	sysstat, ok := sysinfo.(*syscall.Stat_t)
	if !ok{
		return ret,TypeCvtErr
	}
	return time.Unix(sysstat.Atim.Sec,sysstat.Atim.Nsec),nil
}

func getModTime(info os.FileInfo)(time.Time){
	return info.ModTime()
}


func buildcfgmap(cfgstr string){
	log.Println(cfgstr)
}

func loadcfg()error{
	cfginfo,err := os.Stat(cfgfile)
	if err != nil{
		return err
	}
	newmtime := getModTime(cfginfo)
	if newmtime == cfgmtime{
		return nil
	}
	cfgmtime = newmtime
	log.Println("update config from file:",cfgfile)
	dat, err := ioutil.ReadFile(cfgfile)
	if err != nil{
		return err
	}
	buildcfgmap(string(dat))
	return nil
}

func reloadcfg(){
	for{
		time.Sleep(time.Second * 1)
		err := loadcfg()
		if err != nil{
			log.Panic(err)
		}
	}
}

func main(){
	flag.StringVar(&cfgfile,"c","config","config file")
	flag.Parse()
	log.Println("using config file:",cfgfile)
	err := loadcfg()
	if err != nil{
		log.Panic(err)
	}
	go reloadcfg()
	select {}
}
