## 为什么会有很多节点未激活
- 模拟器是否发送了所有对应的入网请求
    - 检查模拟器日志中"send join req"数量
- ns是否收到所有的join req
    - 检查ns日志 "packet(s) collected" 数量
- ns是否发送了所有的join acc
    - 未发送所有的join acc，有join流程出现了问题