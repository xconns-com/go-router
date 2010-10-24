include $(GOROOT)/src/Make.inc

all:	install

install:
	cd trunk && make install
	cd trunk/samples && make
	cd branches/router1 && make install

clean:
	cd trunk && make clean
	cd trunk/samples && make clean
	cd branches/router1 && make clean

nuke:
	cd trunk && make nuke
	cd trunk/samples && make nuke
	cd branches/router1 && make nuke
