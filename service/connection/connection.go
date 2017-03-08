// 根据用户id哈希算法，客户端链接会自动分配到不同的connection server
// 新的connection server被添加后，重新计算哈希，把不属于这个服务器的client重连到其他server，
// 在更新完毕之前有事务需要处理的话，就按照两种哈希的方式分别到两个connection server上查找链接，在发现client的connection server上处理事务
package main

func main() {

}
