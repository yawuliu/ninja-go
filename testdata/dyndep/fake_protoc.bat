@echo off
set OUTPUT=%~n1
echo Generating %OUTPUT%.pb.cc and %OUTPUT%.pb.h
copy nul %OUTPUT%.pb.cc > nul
copy nul %OUTPUT%.pb.h > nul
echo build %OUTPUT%.pb.o: cxx %OUTPUT%.pb.cc > %OUTPUT%.pb.cc.dd