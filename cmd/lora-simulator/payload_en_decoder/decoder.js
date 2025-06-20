function Decode(fPort, bytes) {
    var decoded = {};
    
    // ES6 (ES2015) 语法验证
    try {
        // let/const 声明
        let testLet = 'let_works';
        const testConst = 'const_works';
        decoded.es6_let_const = true;
    } catch (e) {
        decoded.es6_let_const = false;
    }
    
    try {
        // 箭头函数
        const arrowFunc = (x) => x * 2;
        decoded.es6_arrow_function = arrowFunc(5) === 10;
    } catch (e) {
        decoded.es6_arrow_function = false;
    }
    
    try {
        // 模板字符串
        const name = 'World';
        const greeting = `Hello ${name}!`;
        decoded.es6_template_literals = greeting === 'Hello World!';
    } catch (e) {
        decoded.es6_template_literals = false;
    }
    
    try {
        // 解构赋值
        const obj = { a: 1, b: 2 };
        const { a, b } = obj;
        const arr = [1, 2, 3];
        const [first, second] = arr;
        decoded.es6_destructuring = a === 1 && b === 2 && first === 1 && second === 2;
    } catch (e) {
        decoded.es6_destructuring = false;
    }
    
    try {
        // 默认参数
        function testDefault(x = 5) {
            return x;
        }
        decoded.es6_default_parameters = testDefault() === 5;
    } catch (e) {
        decoded.es6_default_parameters = false;
    }
    
    try {
        // rest/spread 语法
        const arr1 = [1, 2, 3];
        const arr2 = [...arr1, 4, 5];
        function restFunc(...args) {
            return args.length;
        }
        decoded.es6_rest_spread = arr2.length === 5 && restFunc(1, 2, 3) === 3;
    } catch (e) {
        decoded.es6_rest_spread = false;
    }
    
    try {
        // 类
        class TestClass {
            constructor(value) {
                this.value = value;
            }
            getValue() {
                return this.value;
            }
        }
        const instance = new TestClass(42);
        decoded.es6_classes = instance.getValue() === 42;
    } catch (e) {
        decoded.es6_classes = false;
    }
    
    try {
        // Promise
        const testPromise = new Promise((resolve) => resolve(true));
        decoded.es6_promises = typeof testPromise.then === 'function';
    } catch (e) {
        decoded.es6_promises = false;
    }
    
    try {
        // Symbol
        const sym = Symbol('test');
        decoded.es6_symbols = typeof sym === 'symbol';
    } catch (e) {
        decoded.es6_symbols = false;
    }
    
    try {
        // Map/Set
        const map = new Map();
        map.set('key', 'value');
        const set = new Set([1, 2, 3]);
        decoded.es6_map_set = map.get('key') === 'value' && set.has(1);
    } catch (e) {
        decoded.es6_map_set = false;
    }
    
    try {
        // for...of 循环
        const testArr = [1, 2, 3];
        let sum = 0;
        for (const item of testArr) {
            sum += item;
        }
        decoded.es6_for_of = sum === 6;
    } catch (e) {
        decoded.es6_for_of = false;
    }
    
    try {
        // 生成器函数
        function* generator() {
            yield 1;
            yield 2;
        }
        const gen = generator();
        decoded.es6_generators = gen.next().value === 1;
    } catch (e) {
        decoded.es6_generators = false;
    }
    
    // ES2016 语法验证
    try {
        // Array.includes()
        const arr = [1, 2, 3];
        decoded.es2016_array_includes = arr.includes(2);
    } catch (e) {
        decoded.es2016_array_includes = false;
    }
    
    try {
        // 指数运算符
        decoded.es2016_exponentiation = 2 ** 3 === 8;
    } catch (e) {
        decoded.es2016_exponentiation = false;
    }
    
    // ES2017 语法验证
    try {
        // async/await (简单测试)
        async function testAsync() {
            return 42;
        }
        decoded.es2017_async_await = typeof testAsync === 'function';
    } catch (e) {
        decoded.es2017_async_await = false;
    }
    
    try {
        // Object.values()/Object.entries()
        const obj = { a: 1, b: 2 };
        const values = Object.values(obj);
        const entries = Object.entries(obj);
        decoded.es2017_object_values_entries = values.length === 2 && entries.length === 2;
    } catch (e) {
        decoded.es2017_object_values_entries = false;
    }
    
    try {
        // String padding
        const str = '5';
        decoded.es2017_string_padding = str.padStart(3, '0') === '005' && str.padEnd(3, '0') === '500';
    } catch (e) {
        decoded.es2017_string_padding = false;
    }
    
    // ES2018 语法验证
    try {
        // Rest/Spread properties for objects
        const obj1 = { a: 1, b: 2 };
        const obj2 = { ...obj1, c: 3 };
        const { a, ...rest } = obj2;
        decoded.es2018_object_rest_spread = obj2.c === 3 && rest.b === 2;
    } catch (e) {
        decoded.es2018_object_rest_spread = false;
    }
    
    try {
        // Promise.finally()
        const p = Promise.resolve(42);
        decoded.es2018_promise_finally = typeof p.finally === 'function';
    } catch (e) {
        decoded.es2018_promise_finally = false;
    }
    
    // ES2019 语法验证
    try {
        // Array.flat()/flatMap()
        const arr = [[1, 2], [3, 4]];
        const flattened = arr.flat();
        const mapped = arr.flatMap(x => x.map(y => y * 2));
        decoded.es2019_array_flat = flattened.length === 4 && mapped[0] === 2;
    } catch (e) {
        decoded.es2019_array_flat = false;
    }
    
    try {
        // Object.fromEntries()
        const entries = [['a', 1], ['b', 2]];
        const obj = Object.fromEntries(entries);
        decoded.es2019_object_fromentries = obj.a === 1 && obj.b === 2;
    } catch (e) {
        decoded.es2019_object_fromentries = false;
    }
    
    try {
        // String.trimStart()/trimEnd()
        const str = '  hello  ';
        decoded.es2019_string_trim = str.trimStart() === 'hello  ' && str.trimEnd() === '  hello';
    } catch (e) {
        decoded.es2019_string_trim = false;
    }
    
    try {
        // Optional catch binding
        try {
            throw new Error('test');
        } catch {
            // 不需要参数的catch
        }
        decoded.es2019_optional_catch_binding = true;
    } catch (e) {
        decoded.es2019_optional_catch_binding = false;
    }
    
    // ES2020 语法验证
    try {
        // Optional chaining
        const obj = { a: { b: { c: 42 } } };
        const nullObj = null;
        decoded.es2020_optional_chaining = obj?.a?.b?.c === 42 && nullObj?.missing === undefined;
    } catch (e) {
        decoded.es2020_optional_chaining = false;
    }
    
    try {
        // Nullish coalescing
        const nullValue = null;
        const undefinedValue = undefined;
        const zeroValue = 0;
        decoded.es2020_nullish_coalescing = (nullValue ?? 'default') === 'default' && 
                                           (undefinedValue ?? 'default') === 'default' && 
                                           (zeroValue ?? 'default') === 0;
    } catch (e) {
        decoded.es2020_nullish_coalescing = false;
    }
    
    try {
        // BigInt
        const bigNum = BigInt(123);
        decoded.es2020_bigint = typeof bigNum === 'bigint' && bigNum === 123n;
    } catch (e) {
        decoded.es2020_bigint = false;
    }
    
    try {
        // globalThis
        decoded.es2020_globalthis = typeof globalThis === 'object';
    } catch (e) {
        decoded.es2020_globalthis = false;
    }
    
    // 添加总结信息
    const supportedFeatures = Object.keys(decoded).filter(key => decoded[key] === true);
    decoded.total_supported_features = supportedFeatures.length;
    decoded.supported_features_list = supportedFeatures;
    
    // 保留原有字段
    decoded.test2 = "test2";
    
    return decoded;
}
