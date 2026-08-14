package input

import "fmt"

const bvAlphabet = "FcwAPNKTMug3GV5Lj7EJnHpWsx4tb8haYeviqBz6rkCy12mUSDQX9RdoZf"
const bvXor int64 = 23442827791579
const bvMask int64 = 2251799813685247
const bvMaxAid int64 = 1 << 51

func AVToBV(aid int64) (string, error) {
	if aid <= 0 || aid >= bvMaxAid {
		return "", fmt.Errorf("aid outside supported range")
	}
	buf := []byte("BV1000000000")
	index := len(buf) - 1
	value := (bvMaxAid | aid) ^ bvXor
	for value > 0 && index >= 3 {
		buf[index] = bvAlphabet[value%58]
		value /= 58
		index--
	}
	buf[3], buf[9] = buf[9], buf[3]
	buf[4], buf[7] = buf[7], buf[4]
	return string(buf), nil
}

func BVToAV(bvid string) (int64, error) {
	if !bvidRE.MatchString(bvid) || len(bvid) != 12 {
		return 0, fmt.Errorf("invalid bvid")
	}
	buf := []byte(bvid)
	buf[3], buf[9] = buf[9], buf[3]
	buf[4], buf[7] = buf[7], buf[4]
	var value int64
	for i := 3; i < len(buf); i++ {
		pos := -1
		for j := 0; j < len(bvAlphabet); j++ {
			if bvAlphabet[j] == buf[i] {
				pos = j
				break
			}
		}
		if pos < 0 {
			return 0, fmt.Errorf("invalid bvid alphabet")
		}
		value = value*58 + int64(pos)
	}
	return (value & bvMask) ^ bvXor, nil
}
