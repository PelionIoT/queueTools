#!/bin/bash


SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do # resolve $SOURCE until the file is no longer a symlink
  SELF="$( cd -P "$( dirname "$SOURCE" )" && pwd )"
  SOURCE="$(readlink "$SOURCE")"
  [[ $SOURCE != /* ]] && SOURCE="$DIR/$SOURCE" # if $SOURCE was a relative symlink, we need to resolve it relative to the path where the symlink file was located
done

THIS_DIR="$( cd -P "$( dirname "$SOURCE" )" && pwd )"

pushd $THIS_DIR

# you need the -P !! --> http://stackoverflow.com/questions/32477816/safely-replacing-text-with-m4-disabling-new-defines



# m4 -P macro-setup.m4 -F m4setup.m4f

if [ ! -z "$DEBUG2" ]; then
	echo "DEBUG2 IS ON!"
	DEBUG2="-D DEBUG_OUT2=fmt.Printf(\"***DEBUG_GO2:\"+\$@) -D IFDEBUG2=\$1"
	# if [ "$DEBUG" -ge "1" ]; then
else
	DEBUG2="-D DEBUG_OUT2=  -D IFDEBUG2=\$2"
fi



if [ ! -z "$DEBUG" ]; then
	echo "DEBUG IS ON!"
	PREPROCESS="m4 -P -D IFDEBUG=\$1 -D NODEBUG= -D DEBUG=\$@ macro-setup.m4 -D DEBUG_OUT=fmt.Printf(\"***DEBUG_GO:\"+\$@) ${DEBUG2}"
else
	PREPROCESS="m4 -P -D IFDEBUG=\$2 -D NODEBUG=\$@ -D DEBUG= -D DEBUG_OUT= macro-setup.m4  ${DEBUG2}"
fi

if [ ! -z "$PRETEND" ]; then
	echo "PRETEND IS ON!"
fi



for f in $(find  src -name '*.go'); do
	echo "--> "$f
#	echo `basename $f`
	FILENAME=`basename $f`
	DIR1=`dirname $f`
	DIR1=${DIR1#*/}
#	echo "------"
	if [ "$DIR1" != "src" ]; then
		echo "mkdir -p $DIR1"
		mkdir -p $DIR1
#		echo "--->"$DIR1"<--"
#		echo "****"
#		echo "output: $DIR1/$FILENAME"
		echo $PREPROCESS "src/$DIR1/$FILENAME" "> $DIR1/$FILENAME"
		if [ -z "$PRETEND" ]; then $PREPROCESS src/$DIR1/$FILENAME > $DIR1/$FILENAME; fi
	else
#		echo "output: $FILENAME"
		echo $PREPROCESS "src/$FILENAME" "> $FILENAME"
		if [ -z "$PRETEND" ]; then $PREPROCESS src/$FILENAME > $FILENAME; fi
	fi
done

for f in $(find  src -name '*\.[ch]'); do
	echo "--> "$f
#	echo `basename $f`
	FILENAME=`basename $f`
	DIR1=`dirname $f`
	DIR1=${DIR1#*/}
#	echo "------"
	if [ "$DIR1" != "src" ]; then
		echo "mkdir -p $DIR1"
		mkdir -p $DIR1
#		echo "--->"$DIR1"<--"
#		echo "****"
#		echo "output: $DIR1/$FILENAME"
		# echo $PREPROCESS "src/$DIR1/$FILENAME" "> $DIR1/$FILENAME"
		# if [ -z "$PRETEND" ]; then $PREPROCESS src/$DIR1/$FILENAME > $DIR1/$FILENAME; fi
		echo "src/$DIR1/$FIELNAME --> $DIR1/$FILENAME"
		cp src/$DIR1/$FILENAME $DIR1/$FILENAME
	else
#		echo "output: $FILENAME"
		# echo $PREPROCESS "src/$FILENAME" "> $FILENAME"
		# if [ -z "$PRETEND" ]; then $PREPROCESS src/$FILENAME > $FILENAME; fi
		echo "src/$FIELNAME --> $FILENAME"
		cp src/$FILENAME $FILENAME
	fi
done


popd

go test -v github.com/WigWagCo/queueTools

