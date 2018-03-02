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
	"strings"
)

type LogCfg struct{
	sync.RWMutex
	logmap map[string][]string//{disk:path}
}


var(
	NoFileSysErr = errors.New("can not get file sys info")
	TypeCvtErr = errors.New("type convert error")
	NotDirErr = errors.New("path is not Dir")

	reloadcfginterval = 1 * time.Second
	checkinterval = 5 * time.Second
	freeperc uint64 = 80

	debug bool
	cfgfile string
	cfgmtime time.Time
	cfg LogCfg
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

func leadwith(s1,s2 string)bool{
	if len(s1) < len(s2){
		return false
	}
	return s1[:len(s2)] == s2
}

func isDir(path string)error{
	info,err := os.Stat(path)
	if err != nil{
		return err
	}
	if ! info.IsDir(){
		return NotDirErr
	}
	return nil
}


func isExist(list []string,rec string)bool{
	for _, v := range list{
		if v == rec{
			return true
		}
	}
	return false
}


func buildcfgmap(cfgstr string,mountpts []string){
	cfg.Lock()
	defer cfg.Unlock()
	cfg.logmap = make(map[string][]string)
	for _,logpath := range strings.Split(cfgstr,"\n"){
		if len(logpath) == 0{
			continue
		}
		if logpath[0] != '/'{
			log.Println("[ERROR]路径必须是绝对路径:",logpath)
			continue
		}
		err := isDir(logpath)
		if err != nil{
			log.Println("[ERROR]:",logpath,err)
			continue
		}
		//build map
		for _,mp := range mountpts{
			if leadwith(logpath,mp){
				_,present := cfg.logmap[mp]
				if !present{
					cfg.logmap[mp] = make([]string,0)
				}
				if ! isExist(cfg.logmap[mp],logpath){
					cfg.logmap[mp] = append(cfg.logmap[mp],logpath)
					log.Println("[add]add",logpath,"to mount point",mp)
				}
				break
			}
		}
	}
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
	mountpints, err := getmountpoint()
	if err != nil{
		return err
	}
	buildcfgmap(string(dat),mountpints)
	return nil
}

func reloadcfg(){
	for{
		time.Sleep(reloadcfginterval)
		err := loadcfg()
		if err != nil{
			log.Panic(err)
		}
	}
}

func getmountpoint()([]string,error){
	dat, err := ioutil.ReadFile("/proc/mounts")
	if err != nil{
		return []string{},err
	}
	recs := strings.Split(string(dat),"\n")
	ans := make([]string,0)
	for i:=0; i < len(recs); i ++{
		sps := strings.Split(recs[i]," ")
		if len(sps) > 2 && strings.Contains(sps[0],"/dev"){
			ans = append(ans,sps[1])
		}
	}
	for i:=0; i < len(ans); i ++{
		for j:=i+1; j < len(ans); j ++{
			if len(ans[i])< len(ans[j]){
				ans[i], ans[j] = ans[j], ans[i]
			}
		}
	}
	return ans,nil
}

func dellog(disk string,logpaths []string){
	log.Println("[info]clear for",disk)
}

func diskperc(disk string)uint64{
	var stat syscall.Statfs_t
	err := syscall.Statfs(disk, &stat)
	if err != nil{
		log.Panic(err)
	}
	return stat.Bfree * 100 /  uint64(stat.Blocks)
}

func checkroutine(){
	for{
		cfg.RLock()
		for disk,logpaths := range cfg.logmap{
			perc := diskperc(disk)
			if perc < freeperc{//free percentage
				log.Println("[info]disk free space for",disk,perc,", begin delete")
				dellog(disk,logpaths)
			}
		}
		cfg.RUnlock()
		time.Sleep(checkinterval)
	}
}


func main(){
	flag.StringVar(&cfgfile,"c","config","config file")
	flag.BoolVar(&debug,"d",false,"debug mode")
	flag.Parse()
	log.Println("using config file:",cfgfile)
	if debug{
		log.Println("[info]enable debug mode")
	}
	cfg.logmap = make(map[string][]string)
	err := loadcfg()
	if err != nil{
		log.Panic(err)
	}
	go reloadcfg()
	checkroutine()
}
